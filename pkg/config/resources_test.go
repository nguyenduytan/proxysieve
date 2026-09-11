package config

import (
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/policy"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
	"github.com/nguyenduytan/proxysieve/pkg/routing"
	"testing"
)

func TestResourcesValidateAndClone(t *testing.T) {
	c := Defaults(t.TempDir())
	c.Proxies = []proxy.Endpoint{{ID: "proxy", Name: "proxy", Protocol: proxy.HTTP, Host: "proxy.example.invalid", Port: 8080, Enabled: true}}
	c.Pools = []routing.Pool{{ID: "pool", Name: "pool", Strategy: routing.RoundRobin, EndpointIDs: []model.ID{"proxy"}, Enabled: true}}
	c.Policies = []policy.Policy{{Version: 1, ID: "default", Name: "default", Rules: []policy.Rule{{ID: "route", Name: "route", Enabled: true, Actions: []policy.Action{{Type: "proxy", PoolID: "pool"}}}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := c.Clone()
	clone.Proxies[0].Tags = []string{"changed"}
	clone.Pools[0].EndpointIDs[0] = "changed"
	clone.Policies[0].Rules[0].Actions[0].PoolID = "changed"
	if c.Pools[0].EndpointIDs[0] != "proxy" || c.Policies[0].Rules[0].Actions[0].PoolID != "pool" {
		t.Fatal("clone aliases")
	}
	c.Pools[0].FallbackPoolIDs = []model.ID{"pool"}
	if c.Validate() == nil {
		t.Fatal("self cycle")
	}
}
