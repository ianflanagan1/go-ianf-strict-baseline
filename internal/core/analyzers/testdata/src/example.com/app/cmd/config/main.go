package main

import (
	"flag"
	"os"

	"github.com/ianflanagan1/go-ianf-strict-shared-code/adapters/driven/env"
)

func main() {
	flag.Parse()
	_ = os.Getenv("TOKEN")
	_ = env.System{}
}
