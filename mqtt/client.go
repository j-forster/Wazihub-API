package mqtt

import "net"

type Client struct {
	id string
}

func Dial(addr string, clientId string, username string, pw string) (*Client, error) {

	client := &Client{clientId}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	go client.serve()
	return client, nil
}

func (client *Client) serve() {

}
