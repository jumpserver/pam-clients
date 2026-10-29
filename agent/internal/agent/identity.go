package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

// Resolve after choosing the state path so read-only deployment configuration
// can be shared by replicas while each replica retains its own identity.
func (c *Config) ensureInstanceID() error {
	if c.InstanceID != "" {
		return nil
	}
	if _, err := os.Lstat(c.StateFile); err == nil {
		if err := privateFile(c.StateFile); err != nil {
			return err
		}
		raw, err := os.ReadFile(c.StateFile)
		if err != nil {
			return err
		}
		var state State
		if err := decodeState(raw, &state); err != nil {
			return err
		}
		if state.InstanceID == "" {
			return errors.New("existing state has no instance_id; configure the original instance_id explicitly")
		}
		c.InstanceID = state.InstanceID
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return errors.New("cannot determine hostname for instance_id")
	}
	var suffix [2]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return errors.New("cannot generate instance_id suffix")
	}
	c.InstanceID = instanceIDForHost(hostname, suffix)
	c.instanceIDGenerated = true
	return nil
}

func instanceIDForHost(hostname string, suffix [2]byte) string {
	hostname = strings.TrimSpace(hostname)
	// The Core/SDK limit is 128 bytes, including the separator and four digits.
	if len(hostname) > 123 {
		hostname = hostname[:123]
		for !utf8.ValidString(hostname) {
			hostname = hostname[:len(hostname)-1]
		}
	}
	return hostname + "-" + hex.EncodeToString(suffix[:])
}
