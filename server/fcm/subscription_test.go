package fcm

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Bogh/gcm"
	"github.com/golang/mock/gomock"
	"github.com/smancke/guble/protocol"
	"github.com/smancke/guble/server/router"
	"github.com/smancke/guble/server/store"
	"github.com/smancke/guble/testutil"
	"github.com/stretchr/testify/assert"
)

var (
	fetchMessage = `/foo/bar,42,user01,phone01,{},1420110000,1
{"Content-Type": "text/plain", "Correlation-Id": "7sdks723ksgqn"}
Hello World`
	dummyFCMResponse = &gcm.Response{
		Results: []gcm.Result{{Error: ""}},
	}
	errorFCMNotRegisteredResponse = &gcm.Response{
		Error:   "NotRegistered",
		Results: []gcm.Result{{Error: "NotRegistered"}},
	}
)

// Test that if a route is closed, but no explicit shutdown the subscription will
// try to re-fetch messages from store and then resubscribe
func TestSub_Restart(t *testing.T) {
	_, finish := testutil.NewMockCtrl(t)
	defer finish()

	a := assert.New(t)

	g, routerMock, storeMock := testSimpleFCM(t, true)

	route := router.NewRoute(router.RouteConfig{
		RouteParams: router.RouteParams{userIDKey: "user01", applicationIDKey: "phone01"},
		Path:        protocol.Path("/foo/bar"),
		ChannelSize: subBufferSize,
	})
	sub := newSubscription(g, route, 2)

	// start goroutine that will take the messages from the pipeline
	done := make(chan struct{})
	go func() {
		for {
			select {
			case pm := <-g.pipelineC:
				pm.resultC <- dummyFCMResponse
			case <-done:
				return
			}
		}
	}()

	storeMock.EXPECT().MaxMessageID(gomock.Eq("foo")).Return(uint64(4), nil).AnyTimes()

	routerMock.EXPECT().Done().Return(make(chan bool)).AnyTimes()

	// simulate the fetch
	routerMock.EXPECT().Fetch(gomock.Any()).Do(func(req *store.FetchRequest) {
		go func() {
			// send 2 messages from the store
			req.StartC <- 2
			var id uint64 = 3
			for i := 0; i < 2; i++ {
				req.MessageC <- &store.FetchedMessage{
					ID:      id,
					Message: []byte(strings.Replace(fetchMessage, "42", strconv.FormatUint(id, 10), 1)),
				}
				id++
			}
			req.Done()
		}()
	})

	// Forcefully close the route so the subscription runs the restart
	route.Close()

	// expect again for a subscription
	routerMock.EXPECT().Subscribe(gomock.Any())

	sub.start()
	time.Sleep(50 * time.Millisecond)

	// subscription route shouldn't be equal anymore
	a.NotEqual(route, sub.route)
	a.Equal(uint64(4), sub.lastID)

	time.Sleep(10 * time.Millisecond)
	close(done)
}

func TestSubscription_JSONError(t *testing.T) {
	_, finish := testutil.NewMockCtrl(t)
	defer finish()

	a := assert.New(t)

	g, routerMock, _ := testSimpleFCM(t, true)
	routerMock.EXPECT().Subscribe(gomock.Any())

	sub, err := initSubscription(g, "/foo/bar", "user01", "gcm01", 0, true)
	a.NoError(err)
	a.Equal(1, len(g.subscriptions))

	// start goroutine that will take the messages from the pipeline
	done := make(chan struct{})
	go func() {
		for {
			select {
			case pm := <-g.pipelineC:
				pm.resultC <- errorFCMNotRegisteredResponse
			case <-done:
				return
			}
		}
	}()

	routerMock.EXPECT().Unsubscribe(gomock.Eq(sub.route))

	sub.route.Deliver(&protocol.Message{
		Path: protocol.Path("/foo/bar"),
		Body: []byte("test message"),
	})

	// subscriptions should be removed at this point
	time.Sleep(time.Second)
	a.Equal(0, len(g.subscriptions))

	close(done)
}