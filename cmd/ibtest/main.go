package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/IS908/optix/internal/broker/ibkr"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := ibkr.New(ibkr.Config{Host: "127.0.0.1", Port: 4001, ClientID: 4})
	if err := c.Connect(ctx); err != nil {
		log.Fatalf("Connect: %v", err)
	}
	defer c.Disconnect()
	fmt.Println("✅ Connected")

	fmt.Println("\n=== AAPL option chain (nearest 4 expirations) ===")
	chain, err := c.GetOptionChain(ctx, "AAPL", "")
	if err != nil {
		log.Fatalf("GetOptionChain: %v", err)
	}

	fmt.Printf("Underlying: %s\n", chain.Underlying)
	fmt.Printf("Expirations: %d\n", len(chain.Expirations))
	for _, exp := range chain.Expirations {
		fmt.Printf("  %-12s  calls:%3d  puts:%3d  strikes: %.0f – %.0f\n",
			exp.Expiration,
			len(exp.Calls),
			len(exp.Puts),
			exp.Calls[0].Strike,
			exp.Calls[len(exp.Calls)-1].Strike,
		)
	}
}
