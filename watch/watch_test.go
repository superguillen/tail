package watch

import (
	"errors"
	"fmt"
	"gopkg.in/tomb.v1"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestWatchNotify(t *testing.T) {
	testCases := []struct {
		name string
		poll bool
	}{
		{"Test watch inotify", false},
		{"Test watch poll", true},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			tmpDir, err := ioutil.TempDir("", "watch-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tmpDir)
			filePath := filepath.Join(tmpDir, "a")
			// create file
			file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0777)
			if err != nil {
				t.Fatal(err)
			}
			err = file.Close()
			if err != nil {
				t.Fatal(err)
			}

			var wg sync.WaitGroup
			var werr error
			changes := 0
			chanClose := make(chan struct{})
			go func() {
				wg.Add(1)
				changes, werr = watchFile(filePath, test.poll, chanClose)
				wg.Done()
			}()

			writeToFile(t, filePath, "hello", true)
			<-time.After(time.Second)
			writeToFile(t, filePath, "world", true)
			<-time.After(time.Second)
			writeToFile(t, filePath, "end", false)
			<-time.After(time.Second)
			//err = os.Remove(filePath)
			//if err != nil {
			//	t.Fatal(err)
			//}
			rmFile(t, filePath)
			chanClose <- struct{}{}
			wg.Wait()
			close(chanClose)

			if werr != nil {
				t.Fatal(werr)
			}
			// ideally, there should be 4 changes (2xmodified,1xtruncaed and 1xdeleted)
			// but, notifications from fsnotify are usually 2 (2xmodify) and 3x from poll (2xmodify, 1xtruncated)
			if changes < 1 || changes > 4 {
				t.Errorf("Invalid changes count: %d\n", changes)
			}
		})
	}
}

func writeToFile(t *testing.T, path, content string, append bool) {
	t.Helper()
	redir := ">"
	if append {
		redir = ">>"
	}
	//var cmd *exec.Cmd
	var out []byte
	var err error
	line := `echo ` + content + " " + redir + path + ``
	if runtime.GOOS == "windows" {
		out, err = exec.Command("cmd", "/c", line).Output()
		//cmd = exec.Command("cmd", "/c", line)
	} else {
		//cmd = exec.Command("sh", "-c", line)
		out, err = exec.Command("sh", "-c", line).Output()
	}
	//fmt.Println(cmd.String())
	//err := cmd.Run()
	if len(out) > 2 {
		fmt.Println("output:", string(out))
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			fmt.Println("Stderr:", string(ee.Stderr))
		}
		t.Fatal(err)
	}
}

func rmFile(t *testing.T, path string) {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "del", path)
	} else {
		cmd = exec.Command("rm", path)
	}

	err := cmd.Run()
	if err != nil {
		t.Fatal(err)
	}
}

func watchFile(path string, poll bool, close <-chan struct{}) (int, error) {
	changesCount := 0
	var mytomb tomb.Tomb
	var watcher FileWatcher
	if poll {
		watcher = NewPollingFileWatcher(path)
	} else {
		watcher = NewInotifyFileWatcher(path)
	}

	for {
		changes, err := watcher.ChangeEvents(&mytomb, 0)
		if err != nil {
			return -1, err
		}
		select {
		case <-changes.Modified:
			fmt.Println("Modified")
			changesCount++
		case <-changes.Deleted:
			fmt.Println("Deleted")
			<-time.After(time.Second)
			if _, err := os.Stat(path); err == nil {
				changesCount++
			}
		case <-changes.Truncated:
			fmt.Println("Truncated")
			changesCount++
		case <-mytomb.Dying():
			return -1, errors.New("dying")
		case <-close:
			goto end
		}
	}
end:
	mytomb.Done()
	return changesCount, nil
}
