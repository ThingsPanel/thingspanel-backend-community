package metrics

import (
	"fmt"
	"testing"
)

func TestInstan(t *testing.T) {
	// Create a new instance
	instance := NewInstance()

	// Call the Instan function
	instance.Instan()

	// Print the collected data
	fmt.Printf("Instance ID: %s\n", instance.InstanceID)
	fmt.Printf("IP Address: %s\n", instance.Address)
	fmt.Printf("Timestamp: %s\n", instance.Timestamp)
	fmt.Printf("Count: %d\n", instance.Count)
}
