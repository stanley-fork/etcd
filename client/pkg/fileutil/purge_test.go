// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestPurgeFile(t *testing.T) {
	dir := t.TempDir()

	// minimal file set
	for i := 0; i < 3; i++ {
		f, ferr := os.Create(filepath.Join(dir, fmt.Sprintf("%d.test", i)))
		require.NoError(t, ferr)
		f.Close()
	}

	stop, purgec := make(chan struct{}), make(chan string, 10)

	// keep 3 most recent files
	errch := purgeFile(zaptest.NewLogger(t), dir, "test", 3, time.Millisecond, stop, purgec, nil, false)
	select {
	case f := <-purgec:
		t.Errorf("unexpected purge on %q", f)
	case <-time.After(10 * time.Millisecond):
	}

	// rest of the files
	for i := 4; i < 10; i++ {
		go func(n int) {
			f, ferr := os.Create(filepath.Join(dir, fmt.Sprintf("%d.test", n)))
			if ferr != nil {
				t.Error(ferr)
			}
			f.Close()
		}(i)
	}

	// watch files purge away
	for i := 4; i < 10; i++ {
		select {
		case <-purgec:
		case <-time.After(time.Second):
			t.Errorf("purge took too long")
		}
	}

	fnames, rerr := ReadDir(dir)
	require.NoError(t, rerr)
	wnames := []string{"7.test", "8.test", "9.test"}
	if !reflect.DeepEqual(fnames, wnames) {
		t.Errorf("filenames = %v, want %v", fnames, wnames)
	}

	// no error should be reported from purge routine
	select {
	case f := <-purgec:
		t.Errorf("unexpected purge on %q", f)
	case err := <-errch:
		t.Errorf("unexpected purge error %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(stop)
}

func TestPurgeFileHoldingLockFile(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 10; i++ {
		var f *os.File
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.test", i)))
		require.NoError(t, err)
		f.Close()
	}

	// create a purge barrier at 5
	p := filepath.Join(dir, fmt.Sprintf("%d.test", 5))
	l, err := LockFile(p, os.O_WRONLY, PrivateFileMode)
	require.NoError(t, err)

	stop, purgec := make(chan struct{}), make(chan string, 10)
	errch := purgeFile(zaptest.NewLogger(t), dir, "test", 3, time.Millisecond, stop, purgec, nil, true)

	for i := 0; i < 5; i++ {
		select {
		case <-purgec:
		case <-time.After(time.Second):
			t.Fatalf("purge took too long")
		}
	}

	fnames, rerr := ReadDir(dir)
	require.NoError(t, rerr)

	wnames := []string{"5.test", "6.test", "7.test", "8.test", "9.test"}
	if !reflect.DeepEqual(fnames, wnames) {
		t.Errorf("filenames = %v, want %v", fnames, wnames)
	}

	select {
	case s := <-purgec:
		t.Errorf("unexpected purge %q", s)
	case err = <-errch:
		t.Errorf("unexpected purge error %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	// remove the purge barrier
	require.NoError(t, l.Close())

	// wait for rest of purges (5, 6)
	for i := 0; i < 2; i++ {
		select {
		case <-purgec:
		case <-time.After(time.Second):
			t.Fatalf("purge took too long")
		}
	}

	fnames, rerr = ReadDir(dir)
	require.NoError(t, rerr)
	wnames = []string{"7.test", "8.test", "9.test"}
	if !reflect.DeepEqual(fnames, wnames) {
		t.Errorf("filenames = %v, want %v", fnames, wnames)
	}

	select {
	case f := <-purgec:
		t.Errorf("unexpected purge on %q", f)
	case err := <-errch:
		t.Errorf("unexpected purge error %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	close(stop)
}
