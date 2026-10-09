//go:build testing

package main

import (
	"testing"

	"github.com/pocketbase/pocketbase/cmd"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppliesMigrations(t *testing.T) {
	pb := getBaseApp()
	root := pb.RootCmd
	// As PocketBase's Start does.
	root.AddCommand(cmd.NewSuperuserCommand(pb), cmd.NewServeCommand(pb, false))
	for args, want := range map[string]bool{
		"serve":                           true,
		"serve --http 0.0.0.0:8090":       true,
		"--dir /data serve":               true,
		"migrate":                         true,
		"migrate up":                      true,
		"migrate down 1":                  false,
		"migrate history-sync":            false,
		"superuser upsert a@b.c password": false,
		"health --url http://x":           false,
	} {
		c, rest, err := root.Find(splitArgs(args))
		require.NoError(t, err, args)
		assert.Equal(t, want, appliesMigrations(c, rest), args)
	}
}

func splitArgs(s string) []string {
	var out []string
	start := -1
	for i, r := range s + " " {
		if r == ' ' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return out
}
