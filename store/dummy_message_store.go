package store

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

const topicSchema = "topic_sequence"

// DummyMessageStore is a minimal implementation of the MessageStore interface.
// Everything it does is storing the message ids in the key value store to
// ensure a monotonic incremented id.
// It is intended for testing and demo purpose, as well as dummy for services without persistence.
// TODO: implement a simple logic to preserve the last N messages
type DummyMessageStore struct {
	topicSequences     map[string]uint64
	topicSequencesLock sync.RWMutex
	kvStore            KVStore
	isSyncStarted      bool

	stopC    chan bool // used to send the stop request to the syc goroutine
	stoppedC chan bool // answer from the syc goroutine, when it is stopped

	idSyncDuration time.Duration
}

func NewDummyMessageStore(kvStore KVStore) *DummyMessageStore {
	return &DummyMessageStore{
		topicSequences: make(map[string]uint64),
		kvStore:        kvStore,
		idSyncDuration: time.Millisecond * 100,
		stopC:          make(chan bool, 1),
		stoppedC:       make(chan bool, 1),
	}
}

func (dms *DummyMessageStore) Start() error {
	go dms.startSequenceSync()
	dms.isSyncStarted = true

	return nil
}

func (dms *DummyMessageStore) Stop() error {
	if !dms.isSyncStarted {
		return nil
	}
	dms.stopC <- true
	<-dms.stoppedC
	return nil
}

func (dms *DummyMessageStore) Store(partition string, msgId uint64, msg []byte) error {
	dms.topicSequencesLock.Lock()
	defer dms.topicSequencesLock.Unlock()
	return dms.store(partition, msgId, msg)
}

func (dms *DummyMessageStore) store(partition string, msgId uint64, msg []byte) error {
	maxId, err := dms.maxMessageId(partition)
	if err != nil {
		return err
	}
	if msgId > 1+maxId {
		return fmt.Errorf("DummyMessageStore: Invalid message id for partition %v. Next id should be %v, but was %q",
			partition, 1+maxId, msgId)
	}
	dms.setId(partition, msgId)
	return nil
}

// Fetch does nothing in this dummy implementation
func (dms *DummyMessageStore) Fetch(req FetchRequest) {
}

func (dms *DummyMessageStore) MaxMessageID(partition string) (uint64, error) {
	dms.topicSequencesLock.Lock()
	defer dms.topicSequencesLock.Unlock()
	return dms.maxMessageId(partition)
}

func (dms *DummyMessageStore) DoInTx(partition string, fnToExecute func(maxMessageId uint64) error) error {
	dms.topicSequencesLock.Lock()
	defer dms.topicSequencesLock.Unlock()
	maxId, err := dms.maxMessageId(partition)
	if err != nil {
		return err
	}
	return fnToExecute(maxId)
}

func (dms *DummyMessageStore) GenerateNextMsgId(msgPathPartition string, timestamp int64) (uint64, error) {
	// TODO MARIAN better implemente this

	return dms.maxMessageId(msgPathPartition)
}

func (dms *DummyMessageStore) maxMessageId(partition string) (uint64, error) {
	sequenceValue, exist := dms.topicSequences[partition]
	if !exist {
		val, existInKVStore, err := dms.kvStore.Get(topicSchema, partition)
		if err != nil {
			return 0, err
		}
		if existInKVStore {
			sequenceValue, _ = strconv.ParseUint(string(val), 10, 0)
		} else {
			sequenceValue = uint64(0)
		}
	}
	dms.topicSequences[partition] = sequenceValue
	return sequenceValue, nil
}

// the the id to a new value
func (dms *DummyMessageStore) setId(partition string, id uint64) {
	dms.topicSequences[partition] = id
}

func (dms *DummyMessageStore) startSequenceSync() {
	lastSyncValues := make(map[string]uint64)
	topicsToUpdate := []string{}

	shouldStop := false
	for !shouldStop {
		select {
		case <-time.After(dms.idSyncDuration):
		case <-dms.stopC:
			shouldStop = true
		}

		dms.topicSequencesLock.Lock()
		topicsToUpdate = topicsToUpdate[:0]
		for topic, seq := range dms.topicSequences {
			if lastSyncValues[topic] != seq {
				topicsToUpdate = append(topicsToUpdate, topic)
			}
		}
		dms.topicSequencesLock.Unlock()

		for _, topic := range topicsToUpdate {
			dms.topicSequencesLock.Lock()
			latestValue := dms.topicSequences[topic]
			dms.topicSequencesLock.Unlock()

			lastSyncValues[topic] = latestValue
			dms.kvStore.Put(topicSchema, topic, []byte(strconv.FormatUint(latestValue, 10)))
		}
	}
	dms.stoppedC <- true
}

func (dms *DummyMessageStore) Check() error {
	return nil
}
