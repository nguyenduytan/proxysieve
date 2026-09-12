package store

import "github.com/nguyenduytan/proxysieve/pkg/auth"

type ClientRecord struct {
	Client   auth.Client `json:"client"`
	Revision int64       `json:"revision"`
}
