package example

import "fmt"

// Account represents a bank account with an ID and balance in cents.
type Account struct {
	ID      string
	Balance int // in cents
}

// Transfer moves amount cents from one account to another.
// Preconditions are enforced via @inco: directives.
func Transfer(from *Account, to *Account, amount int) error {
	// @inco: from != nil
	// @inco: to != nil
	// @inco: from != to, -panic("cannot transfer to self")
	// @inco: amount > 0, -panic("amount must be positive")
	// @inco: from.Balance >= amount, -return(fmt.Errorf("insufficient funds: have %d, need %d", from.Balance, amount))

	from.Balance -= amount
	to.Balance += amount

	fmt.Printf("transferred %d from %s to %s\n", amount, from.ID, to.ID)
	return nil
}
