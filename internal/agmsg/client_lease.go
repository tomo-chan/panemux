package agmsg

import (
	"context"
	"panemux/internal/board"
)

func (c *Client) begin() (func(), error) {
	release := func() {}
	if c.beginCall != nil {
		var err error
		release, err = c.beginCall()
		if err != nil {
			return nil, err
		}
	}
	if c.revision != nil {
		revision := c.revision()
		c.mu.Lock()
		if c.seenRevision != revision {
			c.absent, c.resolvedPath = false, nil
			c.seenRevision = revision
		}
		c.mu.Unlock()
	}
	return release, nil
}

func (c *Client) LeaseMessages() (board.MessageClient, func(), error) {
	release, err := c.begin()
	if err != nil {
		return nil, nil, err
	}
	return leasedClient{c}, release, nil
}
func (c *Client) LeaseParticipation() (board.Participation, func(), error) {
	release, err := c.begin()
	if err != nil {
		return nil, nil, err
	}
	return leasedClient{c}, release, nil
}

type leasedClient struct{ *Client }

func (c leasedClient) Since(ctx context.Context, team, after string, limit int) ([]board.Row, error) {
	return c.since(ctx, team, after, limit)
}
func (c leasedClient) Send(ctx context.Context, team, from, to, body string) error {
	return c.send(ctx, team, from, to, body)
}
func (c leasedClient) Available(ctx context.Context) (bool, error) { return c.available(ctx) }
func (c leasedClient) JoinInstruction(ctx context.Context, request board.JoinRequest) (string, error) {
	return c.joinInstruction(ctx, request)
}

func (c *Client) Since(ctx context.Context, team, after string, limit int) ([]board.Row, error) {
	release, err := c.begin()
	if err != nil {
		return nil, err
	}
	defer release()
	return c.since(ctx, team, after, limit)
}
func (c *Client) Send(ctx context.Context, team, from, to, body string) error {
	release, err := c.begin()
	if err != nil {
		return err
	}
	defer release()
	return c.send(ctx, team, from, to, body)
}
func (c *Client) Available(ctx context.Context) (bool, error) {
	release, err := c.begin()
	if err != nil {
		return false, err
	}
	defer release()
	return c.available(ctx)
}
func (c *Client) JoinInstruction(ctx context.Context, request board.JoinRequest) (string, error) {
	release, err := c.begin()
	if err != nil {
		return "", err
	}
	defer release()
	return c.joinInstruction(ctx, request)
}
