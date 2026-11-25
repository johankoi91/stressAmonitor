//go:build darwin

package worker

import "os"

func fifoBuffer(_ *os.File) (*int, *int, string) { return nil, nil, "" }
