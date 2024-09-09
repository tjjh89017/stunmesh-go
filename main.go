package main

import (
	"context"
	"fmt"
	"log"
)

func main() {
	fmt.Println("Stunmesh Go")
	ctx := context.Background()

	daemon, err := setup()
	if err != nil {
		log.Panic(err)
	}

	daemon.Run(ctx)
}
