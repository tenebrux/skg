// Example: loading an SKG config into Go structs.
//
// Your structs ARE the schema. The `skg:"name"` tag maps config keys
// to struct fields. Nested structs map to blocks. Slices map to arrays.
// Pointer fields are nullable - null in the config leaves them nil.
//
// Run:
//
//	cd examples/go && go run main.go
package main

import (
	"fmt"
	"log"

	skg "github.com/tenebrux/skg/go"
)

// ── The struct IS the schema ───────────────────────────────────────────────
//
// Every field you want from the config needs a `skg:"key"` tag.
// Fields without tags are ignored. Extra keys in the config are ignored.
// This means your struct defines exactly what your app cares about -
// the config can have more, and your code won't break.

type Config struct {
	Name         string                 `skg:"name"`
	Port         int64                  `skg:"port"`
	Debug        bool                   `skg:"debug"`
	AllowedHosts []string               `skg:"allowed_hosts"`
	CacheTTL     *int64                 `skg:"cache_ttl"` // pointer = nullable (null → nil)
	Motd         string                 `skg:"motd"`
	Database     Database               `skg:"database"` // nested struct = block
	Logging      Logging                `skg:"logging"`
	Packages     map[string][]string    `skg:"packages"` // map = block with dynamic keys
	Extra        map[string]interface{} `skg:"extra"`    // map[string]any = arbitrary k/v bag
}

type Database struct {
	Host           string `skg:"host"`
	Port           int64  `skg:"port"`
	Name           string `skg:"name"`
	MaxConnections int64  `skg:"max_connections"`
	SSL            bool   `skg:"ssl"`
}

type Logging struct {
	Level         string  `skg:"level"`
	MaxSizeMB     int64   `skg:"max_size_mb"`
	KeepRotations int64   `skg:"keep_rotations"`
	Streams       Streams `skg:"streams"`
}

type Streams struct {
	Stdout bool `skg:"stdout"`
	File   bool `skg:"file"`
	Syslog bool `skg:"syslog"`
}

func main() {
	// ── Parse and unmarshal in one call ─────────────────────────────────
	var cfg Config
	if err := skg.UnmarshalFile("../app.skg", &cfg); err != nil {
		log.Fatalf("config error: %v", err)
	}

	// ── Use it ─────────────────────────────────────────────────────────
	fmt.Printf("name:         %s\n", cfg.Name)
	fmt.Printf("port:         %d\n", cfg.Port)
	fmt.Printf("debug:        %v\n", cfg.Debug)
	fmt.Printf("hosts:        %v\n", cfg.AllowedHosts)
	fmt.Printf("db:           %s:%d/%s (ssl=%v, pool=%d)\n",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.Name,
		cfg.Database.SSL, cfg.Database.MaxConnections)
	fmt.Printf("log level:    %s\n", cfg.Logging.Level)
	fmt.Printf("log streams:  stdout=%v file=%v syslog=%v\n",
		cfg.Logging.Streams.Stdout, cfg.Logging.Streams.File, cfg.Logging.Streams.Syslog)

	if cfg.CacheTTL == nil {
		fmt.Println("cache_ttl:    <null> (not set)")
	} else {
		fmt.Printf("cache_ttl:    %d\n", *cfg.CacheTTL)
	}

	fmt.Printf("motd:\n%s\n", cfg.Motd)

	// ── Marshal back to SKG ────────────────────────────────────────────
	// Round-trip: struct → SKG text. Proves the struct is the schema in
	// both directions.
	out, err := skg.Marshal(cfg)
	if err != nil {
		log.Fatalf("marshal error: %v", err)
	}
	fmt.Println("\n--- marshaled back to SKG ---")
	fmt.Print(string(out))
}
