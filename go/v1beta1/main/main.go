package main

import (
	"log"

	"github.com/grafeas/grafeas/go/v1beta1/server"
	grafeasStorage "github.com/grafeas/grafeas/go/v1beta1/storage"

	"github.com/grafeas/grafeas-pgsql/go/v1beta1/storage"
)

func main() {
	err := grafeasStorage.RegisterStorageTypeProvider("postgres", storage.PostgresqlStorageTypeProvider)
	if err != nil {
		log.Fatalf("Failed to registering postgres storage provider, %s", err)
	}

	err = server.StartGrafeas()
	if err != nil {
		log.Fatalf("Failed to start Grafeas server, %s", err)
	}
}
