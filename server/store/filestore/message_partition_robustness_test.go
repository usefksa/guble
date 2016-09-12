package filestore

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/smancke/guble/server/store"
	"github.com/stretchr/testify/assert"

	"math"
	"strconv"

	log "github.com/Sirupsen/logrus"
)

func TestMessagePartitionConcurrentWriteAndReads(t *testing.T) {
	t.Parallel()
	// testutil.PprofDebug()
	a := assert.New(t)
	dir, _ := ioutil.TempDir("", "guble_partition_robustness_test")
	defer os.RemoveAll(dir)

	s, _ := newMessagePartition(dir, "myMessages")

	n := 100000
	nReaders := 6

	writerDone := make(chan bool)
	go messagePartitionWriter(a, s, n, writerDone)

	readerDone := make(chan bool)
	for i := 1; i <= nReaders; i++ {
		go messagePartitionReader("reader"+strconv.Itoa(i), a, s, n, readerDone)
	}

	select {
	case <-writerDone:
	case <-time.After(time.Second * 30):
		a.Fail("writer timed out")
	}

	timeout := time.After(time.Second * 30)
	for i := 0; i < nReaders; i++ {
		select {
		case <-readerDone:
		case <-timeout:
			a.Fail("reader timed out")
		}
	}
}

func messagePartitionWriter(a *assert.Assertions, store *messagePartition, n int, done chan bool) {
	for i := 1; i <= n; i++ {
		msg := []byte("Hello " + strconv.Itoa(i))
		a.NoError(store.Store(uint64(i), msg))
	}
	done <- true
}

func messagePartitionReader(name string, a *assert.Assertions, mStore *messagePartition, n int, done chan bool) {
	lastReadMessage := 0

	for lastReadMessage < n {
		msgC := make(chan store.FetchedMessage)
		errorC := make(chan error)

		log.WithFields(log.Fields{
			"module":      "testing",
			"name":        name,
			"lastReadMsg": lastReadMessage + 1,
		}).Debug("Start fetching")

		mStore.Fetch(&store.FetchRequest{
			Partition: "myMessages",
			StartID:   uint64(lastReadMessage + 1),
			Direction: 1,
			Count:     math.MaxInt32,
			MessageC:  msgC,
			ErrorC:    errorC,
			StartC:    make(chan int, 1),
		})

	FETCH:
		for {
			select {
			case msgAndID, open := <-msgC:
				if !open {
					log.WithFields(log.Fields{
						"module":      "testing",
						"name":        name,
						"lastReadMsg": lastReadMessage,
					}).Debug("Stop fetching")
					break FETCH
				}
				a.Equal(lastReadMessage+1, int(msgAndID.ID), "Reader: "+name)
				lastReadMessage = int(msgAndID.ID)
			case err := <-errorC:
				a.Fail("received error", err.Error())
				<-done
				return
			}
		}
	}

	log.WithFields(log.Fields{
		"module":      "testing",
		"name":        name,
		"lastReadMsg": lastReadMessage,
	}).Debug("Ready got id")

	done <- true
}
