package security

import (
	"context"
	"errors"
	"net/netip"
)

var ErrDestinationDenied = errors.New("destination is not allowed")

type Resolver interface {
	LookupNetIP(context.Context, string) ([]netip.Addr, error)
}
type DestinationPolicy struct {
	DenyPrivate  bool
	AllowTrusted bool
}

func (p DestinationPolicy) Allow(addrs []netip.Addr) error {
	if len(addrs) == 0 {
		return ErrDestinationDenied
	}
	if p.AllowTrusted || !p.DenyPrivate {
		return nil
	}
	for _, addr := range addrs {
		if unsafeAddress(addr) {
			return ErrDestinationDenied
		}
	}
	return nil
}
func unsafeAddress(a netip.Addr) bool {
	a = a.Unmap()
	return !a.IsValid() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsPrivate() || a.IsUnspecified() || a.IsMulticast()
}
