package server

import (
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"

	"fmt"
	guble "github.com/smancke/guble/guble"
	"strings"
)

var aTestMessage = &guble.Message{
	Id:   uint64(42),
	Path: "/foo",
	Body: []byte("Test"),
}

func TestSubscriptionMessage(t *testing.T) {
	defer initCtrl(t)()

	messages := []string{"+ /mock", "+ /foo"}
	wsconn, pubSubSource, messageSink := createDefaultMocks(messages)

	pubSubSource.EXPECT().Subscribe(routeMatcher{"/mock"}).Return(nil)
	wsconn.EXPECT().Send([]byte("#subscribed-to /mock"))
	pubSubSource.EXPECT().Subscribe(routeMatcher{"/foo"}).Return(nil)
	wsconn.EXPECT().Send([]byte("#subscribed-to /foo"))

	runNewWsHandler(wsconn, pubSubSource, messageSink)
}

func TestSendMessageWirthPublisherMessageId(t *testing.T) {
	defer initCtrl(t)()

	// given: a send command with PublisherMessageId
	commands := []string{"> /path 42"}
	wsconn, pubSubSource, messageSink := createDefaultMocks(commands)

	messageSink.EXPECT().HandleMessage(gomock.Any()).Do(func(msg *guble.Message) {
		assert.Equal(t, guble.Path("/path"), msg.Path)
		assert.Equal(t, "42", msg.PublisherMessageId)
	})

	wsconn.EXPECT().Send([]byte("#send 42"))

	runNewWsHandler(wsconn, pubSubSource, messageSink)
}

func TestSendMessage(t *testing.T) {
	defer initCtrl(t)()

	commands := []string{"> /path\n\nHello, this is a test"}
	wsconn, pubSubSource, messageSink := createDefaultMocks(commands)

	messageSink.EXPECT().HandleMessage(messageMatcher{path: "/path", message: "Hello, this is a test"})
	wsconn.EXPECT().Send([]byte("#send"))

	runNewWsHandler(wsconn, pubSubSource, messageSink)
}

func TestAnIncommingMessageIsDelivered(t *testing.T) {
	defer initCtrl(t)()

	wsconn, pubSubSource, messageSink := createDefaultMocks([]string{})

	wsconn.EXPECT().Send(aTestMessage.Bytes())

	handler := runNewWsHandler(wsconn, pubSubSource, messageSink)

	handler.messagesToSend <- aTestMessage
	time.Sleep(time.Millisecond * 10)
}

func TestBadCommands(t *testing.T) {
	defer initCtrl(t)()

	badRequests := []string{"XXXX", "", ">", ">/foo", "+", "-", "send /foo"}
	wsconn, pubSubSource, messageSink := createDefaultMocks(badRequests)

	counter := 0

	wsconn.EXPECT().Send(gomock.Any()).Do(func(data []byte) error {
		if strings.HasPrefix(string(data), "#connected") {
			return nil
		}
		if strings.HasPrefix(string(data), "!error-bad-request") {
			counter++
		} else {
			t.Logf("expected bad-request, but got: %v", string(data))
		}
		return nil
	}).AnyTimes()
	runNewWsHandler(wsconn, pubSubSource, messageSink)

	assert.Equal(t, len(badRequests), counter, "expected number of bad requests does not match")
}

func runNewWsHandler(wsconn *MockWSConn, pubSubSource *MockPubSubSource, messageSink *MockMessageSink) *WSHandler {
	handler := NewWSHandler(pubSubSource, messageSink, wsconn, "testuser")
	go func() {
		handler.Start()
	}()
	time.Sleep(time.Millisecond * 10)
	return handler
}

func createDefaultMocks(inputMessages []string) (*MockWSConn, *MockPubSubSource, *MockMessageSink) {
	inputMessagesC := make(chan []byte, 10)
	for _, msg := range inputMessages {
		inputMessagesC <- []byte(msg)
	}

	pubSubSource := NewMockPubSubSource(ctrl)
	messageSink := NewMockMessageSink(ctrl)

	wsconn := NewMockWSConn(ctrl)
	wsconn.EXPECT().Receive(gomock.Any()).Do(func(message *[]byte) error {
		*message = <-inputMessagesC
		return nil
	}).Times(len(inputMessages) + 1)

	wsconn.EXPECT().Send(connectedNotificationMatcher{})

	return wsconn, pubSubSource, messageSink
}

// --- routeMatcher ---------
type routeMatcher struct {
	path string
}

func (n routeMatcher) Matches(x interface{}) bool {
	return n.path == string(x.(*Route).Path)
}

func (n routeMatcher) String() string {
	return "route path equals " + n.path
}

// --- messageMatcher ---------
type messageMatcher struct {
	id      uint64
	path    string
	message string
}

func (n messageMatcher) Matches(x interface{}) bool {
	return n.path == string(x.(*guble.Message).Path) &&
		n.message == string(x.(*guble.Message).Body) &&
		(n.id == 0 || n.id == x.(*guble.Message).Id)
}

func (n messageMatcher) String() string {
	return fmt.Sprintf("message equals %q, %q, %q", n.id, n.path, n.message)
}

// --- Connected Notification Matcher ---------
type connectedNotificationMatcher struct {
}

func (notify connectedNotificationMatcher) Matches(x interface{}) bool {
	return strings.HasPrefix(string(x.([]byte)), "#connected")
}

func (notify connectedNotificationMatcher) String() string {
	return fmt.Sprintf("is connected message")
}