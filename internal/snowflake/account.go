// Package snowflake connects to the Snowflake accounts of customers'
// destinations, into which Changeport merges their sources' tables
// (docs/snowflake.md).
package snowflake

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"

	sf "github.com/snowflakedb/gosnowflake"
)

// Account is how Changeport signs in to a Snowflake account: as a service
// user with a key pair, never a password.
type Account struct {
	// Identifier is the account's identifier, <organization>-<account>.
	Identifier string
	User       string
	Key        *rsa.PrivateKey
	// Role, Warehouse, Database and Schema are every session's, where set;
	// otherwise the user's defaults. Set here rather than with USE, they hold
	// for every connection of the pool.
	Role, Warehouse, Database, Schema string
}

// Open returns a pool of connections to the account. Each connection signs
// in as the user with a token the key signs, and nothing is sent until the
// first query.
//
// A statement waits at most five minutes for a lock, not Snowflake's 12
// hours, so that a lock left behind, such as by a killed sink's open
// transaction, fails the statement rather than stalling it for half a day.
func Open(account Account) *sql.DB {
	lockTimeout := "300"
	return sql.OpenDB(sf.NewConnector(sf.SnowflakeDriver{}, sf.Config{
		Account:       account.Identifier,
		User:          account.User,
		Authenticator: sf.AuthTypeJwt,
		PrivateKey:    account.Key,
		Role:          account.Role,
		Warehouse:     account.Warehouse,
		Database:      account.Database,
		Schema:        account.Schema,
		Params:        map[string]*string{"LOCK_TIMEOUT": &lockTimeout},
	}))
}

// ParsePrivateKey reads an unencrypted RSA key in PKCS#8 PEM, as
// `openssl pkcs8 -topk8 -nocrypt` writes it.
func ParsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("not an unencrypted PKCS#8 private key in PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse the private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("a %T, not an RSA key", key)
	}
	return rsaKey, nil
}
