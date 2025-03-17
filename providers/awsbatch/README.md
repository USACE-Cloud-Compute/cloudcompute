## Container Compute AWS Batch Compute Provider
The container compute aws batch provider is for production compute in AWS

```golang
//create a provider 
cpi := awsbatch.NewAwsBatchProviderInput(
    "arn:aws-us-gov:iam::00000000009:role/ecsTaskExecutionRole",
    "us-east-1",
    "prod_compute",
)

computeProvider, err := awsbatch.NewAwsBatchProvider(cpi)
if err != nil {
    log.Fatal(err)
}

```

