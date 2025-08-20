package cloudcompute

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"strconv"
	"sync"

	"dario.cat/mergo"
	"github.com/google/uuid"
	. "github.com/usace/cc-go-sdk"
)

// EventGenerators provide an iterator type interface to work with sets of events for a Compute.
// old Generator interface was too difficult to synchronize in a concurrent access scenario, so it
// was simplified below to a single call that can be managed within one mutex
// List type event generators will return new and separate manifests for each event
// All others (array, streaming) will return the same manifests but with a unique event identifier for
// each event which allows for shared payloads (write one and use for every event)
type EventGeneratorOld interface {
	HasNextEvent() bool
	NextEvent() (Event, error)
}

// EventGenerators provide an iterator type interface to work with sets of events for a Compute.
type EventGenerator interface {
	NextEvent() (Event, bool, error)
}

type StreamingEventGenerator struct {
	event   Event
	scanner *bufio.Scanner
	index   int
	mu      sync.Mutex
}

func NewStreamingEventGeneratorForReader(event Event, reader io.Reader, delimiter string) (*StreamingEventGenerator, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(splitAt(delimiter))
	manifestCount := len(event.Manifests)
	for i := 0; i < manifestCount; i++ {
		err := event.Manifests[i].WritePayload()
		if err != nil {
			return nil, fmt.Errorf("failed to write payload for manifest %s: %s", event.Manifests[i].ManifestID, err)
		}
	}
	return &StreamingEventGenerator{
		event:   event,
		scanner: scanner,
	}, nil
}

func NewStreamingEventGenerator(event Event, scanner *bufio.Scanner) (*StreamingEventGenerator, error) {
	manifestCount := len(event.Manifests)
	for i := 0; i < manifestCount; i++ {
		err := event.Manifests[i].WritePayload()
		if err != nil {
			return nil, fmt.Errorf("failed to write payload for manifest %s: %s", event.Manifests[i].ManifestID, err)
		}
	}
	return &StreamingEventGenerator{
		event:   event,
		scanner: scanner,
	}, nil
}

// func (seg *StreamingEventGenerator) HasNextEvent() bool {
// 	seg.mu.Lock()
// 	defer seg.mu.Unlock()
// 	return seg.scanner.Scan()
// }

func (seg *StreamingEventGenerator) NextEvent() (Event, bool, error) {
	seg.mu.Lock()
	defer seg.mu.Unlock()
	hasNext := seg.scanner.Scan()
	event := seg.event
	eventId := seg.scanner.Text()
	if eventId == "" {
		return event, hasNext, fmt.Errorf("empty event identifier at index: %d", seg.index)
	}
	seg.index++
	event.EventIdentifier = eventId
	return event, hasNext, nil
}

type ArrayEventGenerator struct {
	event    Event
	end      int64
	position int64
	mu       sync.Mutex
}

func NewArrayEventGenerator(event Event, start int64, end int64) (*ArrayEventGenerator, error) {
	manifestCount := len(event.Manifests)

	//order the set of manifests
	//@TODO...need to order all event generator manifest sets!!!
	if manifestCount > 1 {
		orderedIds, err := event.TopoSort()
		if err != nil {
			log.Printf("Unable to order event %s: %s\n", event.ID, err)
		}
		orderedManifests := make([]ComputeManifest, len(event.Manifests))
		for i, oid := range orderedIds {
			orderedManifests[i], err = getManifest(event.Manifests, oid)
			if err != nil {
				log.Printf("Unable to order event %s: %s\n", event.ID, err)
			}
		}
		event.Manifests = orderedManifests
	}

	for i := 0; i < manifestCount; i++ {
		err := event.Manifests[i].WritePayload()
		if err != nil {
			return nil, fmt.Errorf("failed to write payload for manifest %s: %s", event.Manifests[i].ManifestID, err)
		}
	}
	return &ArrayEventGenerator{
		event:    event,
		position: start,
		end:      end,
	}, nil
}

// func (aeg *ArrayEventGenerator) HasNextEvent() bool {
// 	aeg.mu.Lock()
// 	defer aeg.mu.Unlock()
// 	return aeg.position <= aeg.end
// }

func (aeg *ArrayEventGenerator) NextEvent() (Event, bool, error) {
	aeg.mu.Lock()
	defer aeg.mu.Unlock()
	event := aeg.event
	event.EventIdentifier = strconv.Itoa(int(aeg.position))
	hasNext := aeg.position <= aeg.end
	aeg.position++
	return event, hasNext, nil
}

// EventList is an EventGenerator composed of a slice of events.
// Events are enumerated in the order they were placed in the slice.
type EventList struct {
	currentEvent int
	events       []Event
	mu           sync.Mutex
}

// Instantiates a new EventList
func NewEventList(events []Event) *EventList {
	el := EventList{
		currentEvent: -1,
		events:       events,
	}
	return &el
}

// Determines if all of the events have been enumerated
// func (el *EventList) HasNextEvent() bool {
// 	el.mu.Lock()
// 	defer el.mu.Unlock()
// 	el.currentEvent++
// 	return el.currentEvent < len(el.events)
// }

//@TODO: Could optimize the sorting an instead of returning a list of ordered IDs, return a sorted list of manifests.....

// Retrieves the next event.  Attempts to perform a topological sort on the manifest slice before returning.
// If sort fails it will log the issue and return the unsorted manifest slice
func (el *EventList) NextEvent() (Event, bool, error) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.currentEvent++
	hasNext := el.currentEvent < len(el.events)
	event := el.events[el.currentEvent]
	if event.EventIdentifier == "" {
		event.EventIdentifier = strconv.Itoa(el.currentEvent)
	}
	return event, hasNext, nil
}

type BatchEvent struct {
	EventIdentifier   string          `json:"eventIdentifier"`
	ManifestOverrides ComputeManifest `json:"manifest"`
}

type BatchEventGenerator struct {
	event Event
	batch []BatchEvent
	index int
	size  int
}

func NewBatchEventGenerator(event Event, batch []BatchEvent) *BatchEventGenerator {
	return &BatchEventGenerator{
		event: event,
		batch: batch,
		size:  len(batch),
	}
}

// func (beg *BatchEventGenerator) HasNextEvent() bool {
// 	return beg.index+1 <= beg.size
// }

// @TODO how to handle error in set of events?
func (beg *BatchEventGenerator) NextEvent() (Event, bool, error) {
	hasNext := beg.index+1 <= beg.size
	batchevent := beg.batch[beg.index]
	computeManifests := ComputeManifests(beg.event.Manifests)
	mergedManifests := make([]ComputeManifest, computeManifests.Len())
	for i := 0; i < computeManifests.Len(); i++ {
		manifest, err := computeManifests.GetManifestByIndex(i, true) //get a deep copy of the manifest
		if err != nil {
			return Event{}, hasNext, err
		}
		mergedManifest, err := mergeComputeManifest(*manifest, batchevent.ManifestOverrides)
		if err != nil {
			return Event{}, hasNext, err
		}
		mergedManifests[i] = mergedManifest
	}
	beg.index++
	nextEvent := Event{
		ID:              uuid.New(),
		EventIdentifier: batchevent.EventIdentifier,
		Manifests:       mergedManifests,
	}
	return nextEvent, hasNext, nil
}

// ///////////////////////////////////////
// /private functions
// ///////////////////////////////////////

func mergePayloadAttributes(dst, src PayloadAttributes) PayloadAttributes {
	if dst == nil {
		dst = make(PayloadAttributes)
	}
	for key, value := range src {
		if _, exists := dst[key]; !exists || value != nil {
			dst[key] = value
		}
	}
	return dst
}

func mergeComputeManifest(dst, src ComputeManifest) (ComputeManifest, error) {
	//Merge PayloadAttribute maps manually
	dst.Inputs.PayloadAttributes = mergePayloadAttributes(dst.Inputs.PayloadAttributes, src.Inputs.PayloadAttributes)
	for i := range dst.Actions {
		dst.Actions[i].Attributes = mergePayloadAttributes(dst.Actions[i].Attributes, src.Actions[i].Attributes)
	}

	// Use mergo to merge other fields
	if err := mergo.Merge(&dst, src, mergo.WithTransformers(uuidTransformer{})); err != nil {
		return ComputeManifest{}, err
	}

	return dst, nil
}

// mergo does not properly recognize zero but non nil value UUIDs as empty
// and therefore will not overwrite them .  The uuidTransfoermer modifies mergo
// to replace zero uuid values in the destination with non-zero values in the src
type uuidTransformer struct{}

func (t uuidTransformer) Transformer(typ reflect.Type) func(dst, src reflect.Value) error {
	if typ == reflect.TypeOf(uuid.UUID{}) {
		return func(dst, src reflect.Value) error {
			if dst.CanSet() {
				if src.Interface().(uuid.UUID) != uuid.Nil {
					dst.Set(src)
				}
			}
			return nil
		}
	}
	return nil
}

func mergeUUID(dst, src reflect.Value) {
	if src.Interface().(uuid.UUID) != uuid.Nil {
		dst.Set(src)
	}
}

func getManifest(manifests []ComputeManifest, id uuid.UUID) (ComputeManifest, error) {
	for _, m := range manifests {
		if m.ManifestID == id {
			return m, nil
		}
	}
	return ComputeManifest{}, errors.New("Unable to find Manifest in list")
}

func splitAt(substring string) func(data []byte, atEOF bool) (advance int, token []byte, err error) {
	searchBytes := []byte(substring)
	searchLen := len(searchBytes)
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		dataLen := len(data)

		// Return nothing if at end of file and no data passed
		if atEOF && dataLen == 0 {
			return 0, nil, nil
		}

		// Find next separator and return token
		if i := bytes.Index(data, searchBytes); i >= 0 {
			return i + searchLen, data[0:i], nil
		}

		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return dataLen, data, nil
		}

		// Request more data.
		return 0, nil, nil
	}
}
