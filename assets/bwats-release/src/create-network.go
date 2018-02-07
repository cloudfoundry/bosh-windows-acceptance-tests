package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/Microsoft/hcsshim"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	octet := rand.Intn(255)
	network := &hcsshim.HNSNetwork{
		Name:    fmt.Sprintf("bwats-net-%d", rand.Intn(10000)),
		Type:    "nat",
		Subnets: []hcsshim.Subnet{{AddressPrefix: fmt.Sprintf("192.168.%d.0/24", octet), GatewayAddress: fmt.Sprintf("192.168.%d.1", octet)}},
	}
	if createdNetwork, err := network.Create(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create network: %s\n", err)
		os.Exit(1)
	} else {
		if marshaled, err := json.Marshal(createdNetwork); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to JSON marshal information for network %#v: %s\n", createdNetwork, err)
			os.Exit(1)
		} else {
			fmt.Printf("%s\n", marshaled)
		}
	}
}
