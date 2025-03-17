## Container Compute Docker Provider
The container compute docker provider is intended to support local development of plugins.  

```golang
//create a provider with a job execution concurrency of 4:
computeProvider := NewDockerComputeProvider(DockerComputeProviderConfig{
	Concurrency: 4,
})

//create a provider with a secrets manager
sm := NewSecretManager("")

sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_ACCESS_KEY_ID::", "ASFASDFASDFASDF")
sm.AddSecret("arn:local:secretsmanager:secret:prod/CloudCompute/ASDFASDF:AWS_SECRET_ACCESS_KEY::", "ASDFFASDFASDFASDF")

computeProvider2 := NewDockerComputeProvider(DockerComputeProviderConfig{
    Concurrency:    1,
    SecretsManager: sm,
})

```
