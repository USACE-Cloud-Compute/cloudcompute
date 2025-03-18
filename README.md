## Cloud Compute

Cloud Compute is a framework designed for large-scale computing, drawing significant inspiration from AWS Batch. It structures compute operations into the following key components:

 - Compute Environments – Define the execution environment for jobs, including resources and constraints.
 - Job Definitions – Specify job configurations such as container images, resource requirements, and execution parameters.
 - Job Queues – Manage the prioritization and scheduling of jobs across compute environments.
 - Jobs – The actual units of work submitted to the system for execution.

## Core Principles
Cloud Compute key definitions and principles are listed below:
 - ## Plugins
   - Compute Plugins: The containerized code executed as jobs within the environment.
   ### Plugins and Execution Model
   Plugins are central to Cloud Compute. They function as externally developed software packages integrated into the framework. Key characteristics:
    - Input Handling – Plugins accept an input payload or environment variables defining execution parameters.
    - Execution Model – A plugin runs once its required inputs are available, as determined by the DAG structure.
    - Computational Scope – A plugin's functionality can range from simple tasks, such as generating random numbers, to complex models, such as physics-based watershed simulations.

   ### Plugin Principals
   - Compute Plugins are fully containerized and operate independently without awareness of other plugins.
   - Plugins can be written in any programming language.
   - Each Compute Plugin must define a Compute Manifest Schema, specifying its input and output requirements. Details can be found in the Plugins documentation
   - Plugins undergo an approval process before deployment, which varies by environment but addresses computational, licensing, Department of Defense (DoD) policy, and cybersecurity concerns.
   - Plugins communicate execution status and results via logging to STDOUT and STDERR or MQTT messaging.
   - Plugins can only execute on resources provisioned by Cloud Compute and cannot spawn external Compute Plugins.
   - Resource Access: 
     - Compute Plugins can read input from data sources that they have authorization to access
     - Compute Plugins write output to the data sources they have authorization to access
   - Compute Plugins will be responsible for identifying error conditions within the compute job and dumping debug info to either a file store and reporting the error.

 - ## Cloud Compute:
   - Will manage job execution by
     - Provisioning plugin resources
     - Pushing jobs to an internal queue to submit to a compute provider
   - Is responsible for scaling events horizontally on compute environments.

## Events and Computational Flow
Cloud Compute introduces the concept of an `Event`, which represents a circuit through the directed acyclic graph (DAG). This DAG defines the computational sequence for a given event. Key properties include:

 - Parallel Execution – Events can be executed in parallel, and nodes within the DAG can run concurrently subject to dependency constraints.
 - Manifests – A single plugin execution within an event is called a `Manifest`. An event consists of multiple manifests which can have dependencies on each other.

## Implementations
Cloud Compute currently has two implementations:
 - AWS Batch Integration – Designed for scalable, cloud-based compute workloads.
 - Local Docker Compute – Primarily used for plugin development and testing.

## Development Guidelines
 - Cloud Compute licensing is MIT
 - Core libraries are written in golang, however compute plugins can be written in any language.
 - Plugins should avoid proprietary licensing, and licensing terms will potentially prevent deployment of a plugin within the cloud environment.
 - Plugins will be subject to a vetting process
 - Plugins can not contain Personably Identifiable Information (PII), Personal Health Information (PHI), or Controlled Unclassified Information (CUI)
 - Cloud Compute Events (DAGS) can only be constructed from approved plugins

## License
MIT License



## Software Development Kits
The software development kit (SDK) provides the essential data structures and a handful of utility services to provide the necessary consistency needed for cloud compute.

GO : https://github.com/USACE/cc-go-sdk

Java : https://github.com/USACE/cc-java-sdk

Python : https://github.com/USACE/cc-python-sdk

DotNet : https://github.com/USACE/cc-dotnet-sdk




