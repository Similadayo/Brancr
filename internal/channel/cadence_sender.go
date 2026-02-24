package channel

import "time"

func SendWithCadence(send func(string), chunks []string) {
	if len(chunks) == 0 {
		return
	}

	send(chunks[0])

	if len(chunks) > 1 {
		// 300-600ms delay. Fixed at 400ms for determinism.
		time.Sleep(400 * time.Millisecond)
		send(chunks[1])
	}
}
