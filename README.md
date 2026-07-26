# run.cloud Go SDK

```bash
go get github.com/newly-app/run-cloud-go
export RUN_CLOUD_API_KEY="rc_live_..."
```

```go
package main

import (
	"context"
	"fmt"
	"log"

	runcloud "github.com/newly-app/run-cloud-go"
)

func main() {
	client, err := runcloud.NewClient(runcloud.ClientOptions{})
	if err != nil {
		log.Fatal(err)
	}
	box, err := client.CreateSandbox(context.Background(), runcloud.CreateSandboxOptions{
		Image:  "runcloud/agent-base",
		CPU:    2,
		Memory: 4096,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer box.Destroy(context.Background())

	result, err := box.Exec(context.Background(), "go version", runcloud.ExecOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(result.Stdout)
}
```

The Go package is the native run.cloud API. Provider-shaped compatibility
adapters are published only for languages where the corresponding upstream SDK
defines that API: TypeScript in `@run-cloud/sdk` and Python in `runcloud`.
