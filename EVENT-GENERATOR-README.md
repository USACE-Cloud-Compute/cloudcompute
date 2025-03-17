## Event Generators
In Container Compute, an event represents a single execution through a Directed Acyclic Graph (DAG). Event Generators streamline the creation of large numbers of events for stochastic simulations, making the process more efficient.

The event generator interface contains two methods:
```golang
type EventGenerator interface {
	HasNextEvent() bool
	NextEvent() (Event, error)
}
```

Currently Container Compute has three types of Event Generators:

### List Event Generator
A list event generator is an event generator that contains a list of events that will be run sequentually.  Each event in a list event generator is independant of other evednts, therefore the dags in each event can be different.

```golang
eventGenerator := NewEventList([]Event{
    {
        ID:              uuid.New(),
        EventIdentifier: strconv.Itoa(event),
        Manifests:       []ComputeManifest{manifest1,manifest2,manifest3},
    },
})
```

### Array Event Generator
An array event generator includes a start event number, an end event number, and a single event that executes for each iteration of the array values. During each iteration, the array value is injected into the event as the CC_EVENT_IDENTIFIER.

```golang
eventGenerator, err := NewArrayEventGenerator(event, startEvent, endEvent)
if err != nil {
    log.Fatalln(err)
}
```

### StreamingEventGenerator
A streaming event generator takes a tokenizable stream of data and processes each token into a compute job with the token as the CC_EVENT_IDENTIFIER value.  An example use case would be to process a set of events that meet specific criteria.

```golang
//in this example we have a comma separated list of event numbers and we want to run the event for each event number.  e.g. 3,12,100,150, etc
file, err := os.Open(csvEventsFilePath)
if err != nil {
    log.Fatalln(err)
}
defer file.Close()

scanner := bufio.NewScanner(file)
scanner.Split(splitAt(","))
eventGenerator, err := NewStreamingEventGenerator(event, scanner)
if err != nil {
    log.Fatalln(err)
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
```

### Batch Event Generator
A batch event generator takes a single event and executes multiple runs, applying specific overrides to each run within the batch.

```golang

```

