package channel

import "fmt"

// IntegrationStub demonstrates how to use the chunker and sender.
// This ensures that the code compiles and provides a clear integration pattern.
func IntegrationStub() {
	// Mock send function
	sendFn := func(msg string) {
		fmt.Println("Sending:", msg)
	}

	// Example usage
	text := "This is a natural response. It has two parts."
	chunks := ChunkMessage(text)

	SendWithCadence(sendFn, chunks)
}
