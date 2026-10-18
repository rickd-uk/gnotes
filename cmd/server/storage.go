package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"gnotes/internal/storage"
)

func databaseKey() ([]byte, error) {
	path := os.Getenv("GNOTES_DATABASE_KEY_FILE")
	if path != "" {
		return readDatabaseKey(path)
	}
	if os.Getenv("GNOTES_REQUIRE_ENCRYPTION") == "1" || os.Getenv("GNOTES_VOLATILE_KEY") == "1" {
		return nil, errors.New("database encryption is required; supply GNOTES_DATABASE_KEY_FILE")
	}
	return nil, nil
}

func readDatabaseKey(path string) ([]byte, error) {
	if os.Getenv("GNOTES_VOLATILE_KEY") == "1" {
		if err := storage.CheckVolatileKey(path); err != nil {
			return nil, err
		}
	}
	return storage.ReadKey(path)
}

// Database commands operate on explicit files, never start HTTP, and never
// initialize or migrate the application schema during verification/recovery.
func runDatabaseCommand(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: gnotes db keygen|encrypt|snapshot|verify [options]")
	}
	flags := flag.NewFlagSet("gnotes db "+args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	source := flags.String("source", "", "source database file")
	destination := flags.String("output", "", "new destination file (must not exist)")
	keyFile := flags.String("key-file", os.Getenv("GNOTES_DATABASE_KEY_FILE"), "private database key file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected database command arguments")
	}
	if args[0] == "keygen" {
		if *destination == "" || *source != "" {
			return errors.New("keygen requires --output KEY_FILE")
		}
		if err := storage.GenerateKey(*destination); err != nil {
			return err
		}
		fmt.Fprintln(output, "Database recovery key created. Keep a separate offline copy; key contents are never printed.")
		return nil
	}
	if args[0] != "encrypt" && args[0] != "snapshot" && args[0] != "verify" {
		return errors.New("unknown database command")
	}
	if *source == "" {
		return errors.New("database command requires --source DATABASE_FILE")
	}
	if *keyFile == "" && (os.Getenv("GNOTES_REQUIRE_ENCRYPTION") == "1" || os.Getenv("GNOTES_VOLATILE_KEY") == "1") {
		return errors.New("database encryption is required; supply --key-file")
	}
	var key []byte
	if *keyFile != "" {
		var err error
		key, err = readDatabaseKey(*keyFile)
		if err != nil {
			return err
		}
		defer clear(key)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if args[0] == "verify" {
		if *destination != "" {
			return errors.New("verify does not accept an output file")
		}
		database, err := storage.Open(*source, key, "ro")
		if err != nil {
			return err
		}
		defer database.Close()
		if err := storage.Verify(database); err != nil {
			return err
		}
		fmt.Fprintln(output, "Database integrity and schema verified.")
		return nil
	}
	if len(key) != 32 || *destination == "" {
		return errors.New("encrypt/snapshot requires --key-file KEY_FILE and --output NEW_DATABASE_FILE")
	}
	var sourceKey []byte
	if args[0] == "snapshot" {
		sourceKey = key
	}
	if err := storage.Copy(ctx, *source, sourceKey, *destination, key); err != nil {
		return err
	}
	fmt.Fprintln(output, "Verified encrypted database created; source is unchanged.")
	return nil
}
