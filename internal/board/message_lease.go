package board

import "fmt"

// MessageLeaser admits an entire consumer operation. The leased client bypasses
// admission for nested script calls while the operation drains before updates.
type MessageLeaser interface {
	LeaseMessages() (MessageClient, func(), error)
}

func leaseMessages(client MessageClient) (MessageClient, func(), error) {
	if leaser, ok := client.(MessageLeaser); ok {
		leased, release, err := leaser.LeaseMessages()
		if err != nil {
			return nil, nil, fmt.Errorf("admit messages: %w", err)
		}
		return leased, release, nil
	}
	return client, func() {}, nil
}

type ParticipationLeaser interface {
	LeaseParticipation() (Participation, func(), error)
}
