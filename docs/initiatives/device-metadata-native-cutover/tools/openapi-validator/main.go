package main

import (
	"fmt"
	"os"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi/datamodel"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: openapi-validator <openapi.yaml>")
		os.Exit(2)
	}

	spec, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read OpenAPI document:", err)
		os.Exit(1)
	}

	// Recursive arrays are valid JSON Schema shapes for bounded trees. Ignore
	// only array-recursion detection; all other reference and OAS checks remain on.
	document, err := libopenapi.NewDocumentWithConfiguration(spec, &datamodel.DocumentConfiguration{
		IgnoreArrayCircularReferences: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse OpenAPI document:", err)
		os.Exit(1)
	}

	openapiValidator, errs := validator.NewValidator(document)
	if len(errs) != 0 {
		fmt.Fprintln(os.Stderr, "create OpenAPI validator:", errs)
		os.Exit(1)
	}
	valid, validationErrors := openapiValidator.ValidateDocument()
	if !valid {
		fmt.Fprintln(os.Stderr, "OpenAPI validation errors:", validationErrors)
		os.Exit(1)
	}

	fmt.Println("document passes all OpenAPI validations")
}
