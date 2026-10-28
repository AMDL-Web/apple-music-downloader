package wrapper

import (
	"context"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/AMDL-Web/wrapper-manager/proto"
	"google.golang.org/grpc/metadata"
)

// fakeDecryptStream is a minimal pb.WrapperManagerService_DecryptClient whose
// Send blocks until the session context is cancelled, modelling a send stuck on
// the gRPC flow-control window, and whose Recv fails immediately.
type fakeDecryptStream struct {
	ctx         context.Context
	recvErr     error
	sendReturns atomic.Int32
	closeSends  atomic.Int32
}

type blockingDecryptStream struct {
	ctx             context.Context
	sendActive      atomic.Int32
	sendReturns     atomic.Int32
	closeSends      atomic.Int32
	closeDuringSend atomic.Int32
}

func (f *blockingDecryptStream) Send(*pb.DecryptRequest) error {
	f.sendActive.Add(1)
	defer f.sendActive.Add(-1)
	<-f.ctx.Done()
	f.sendReturns.Add(1)
	return f.ctx.Err()
}
func (f *blockingDecryptStream) Recv() (*pb.DecryptReply, error) {
	<-f.ctx.Done()
	return nil, f.ctx.Err()
}
func (f *blockingDecryptStream) Header() (metadata.MD, error) { return nil, nil }
func (f *blockingDecryptStream) Trailer() metadata.MD         { return nil }
func (f *blockingDecryptStream) CloseSend() error {
	f.closeSends.Add(1)
	if f.sendActive.Load() != 0 {
		f.closeDuringSend.Add(1)
	}
	return nil
}
func (f *blockingDecryptStream) Context() context.Context { return f.ctx }
func (f *blockingDecryptStream) SendMsg(any) error        { return nil }
func (f *blockingDecryptStream) RecvMsg(any) error        { return nil }

type echoDecryptStream struct {
	ctx      context.Context
	requests chan *pb.DecryptRequest
}

func (f *echoDecryptStream) Send(req *pb.DecryptRequest) error {
	select {
	case f.requests <- req:
		return nil
	case <-f.ctx.Done():
		return f.ctx.Err()
	}
}
func (f *echoDecryptStream) Recv() (*pb.DecryptReply, error) {
	select {
	case req := <-f.requests:
		return &pb.DecryptReply{Header: &pb.ReplyHeader{Code: 0}, Data: req.GetData()}, nil
	case <-f.ctx.Done():
		return nil, f.ctx.Err()
	}
}
func (f *echoDecryptStream) Header() (metadata.MD, error) { return nil, nil }
func (f *echoDecryptStream) Trailer() metadata.MD         { return nil }
func (f *echoDecryptStream) CloseSend() error             { return nil }
func (f *echoDecryptStream) Context() context.Context     { return f.ctx }
func (f *echoDecryptStream) SendMsg(any) error            { return nil }
func (f *echoDecryptStream) RecvMsg(any) error            { return nil }

func (f *fakeDecryptStream) Send(*pb.DecryptRequest) error {
	<-f.ctx.Done() // unblocks only once DecryptFragment cancels the session
	f.sendReturns.Add(1)
	return f.ctx.Err()
}
func (f *fakeDecryptStream) Recv() (*pb.DecryptReply, error) { return nil, f.recvErr }
func (f *fakeDecryptStream) Header() (metadata.MD, error)    { return nil, nil }
func (f *fakeDecryptStream) Trailer() metadata.MD            { return nil }
func (f *fakeDecryptStream) CloseSend() error                { f.closeSends.Add(1); return nil }
func (f *fakeDecryptStream) Context() context.Context        { return f.ctx }
func (f *fakeDecryptStream) SendMsg(any) error               { return nil }
func (f *fakeDecryptStream) RecvMsg(any) error               { return nil }

// TestDecryptFragmentWaitsForSenderOnRecvError verifies that when Recv fails
// while a Send is still in flight, DecryptFragment cancels the stream and waits
// for the sender to finish before returning — so the subsequent Close
// (CloseSend) cannot race a concurrent Send on the same stream.
func TestDecryptFragmentWaitsForSenderOnRecvError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeDecryptStream{ctx: ctx, recvErr: errors.New("recv boom")}
	session := &grpcDecryptSession{stream: fake, cancel: cancel, adamID: "adam"}

	done := make(chan error, 1)
	go func() {
		_, err := session.DecryptFragment("key", [][]byte{{1, 2, 3}})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DecryptFragment returned nil, want the recv error")
		}
		// The sender must have been unblocked (cancelled) and drained before
		// return; otherwise a Send would still be running when Close is called.
		if got := fake.sendReturns.Load(); got != 1 {
			t.Fatalf("sender still in flight after return: sendReturns=%d, want 1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DecryptFragment hung; it did not cancel and drain the blocked sender")
	}

	// Close now runs CloseSend with no Send in flight — the race the fix closes.
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if fake.closeSends.Load() != 1 {
		t.Fatalf("CloseSend called %d times, want 1", fake.closeSends.Load())
	}
}

func TestDecryptFragmentTimeoutCancelsAndDrainsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &blockingDecryptStream{ctx: ctx}
	session := &grpcDecryptSession{
		stream: fake, cancel: cancel, adamID: "adam", fragmentTimeout: 20 * time.Millisecond,
	}

	started := time.Now()
	_, err := session.DecryptFragment("key", [][]byte{{1, 2, 3}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DecryptFragment error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("fragment timeout took %s, want prompt cancellation", elapsed)
	}
	if got := fake.sendReturns.Load(); got != 1 {
		t.Fatalf("sender returns = %d, want 1 before DecryptFragment returns", got)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := fake.closeSends.Load(); got != 1 {
		t.Fatalf("CloseSend called %d times, want 1", got)
	}
	if got := fake.closeDuringSend.Load(); got != 0 {
		t.Fatalf("CloseSend overlapped %d active Send calls, want 0", got)
	}
}

func TestDecryptTimeoutIsPerFragmentNotWholeSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &echoDecryptStream{ctx: ctx, requests: make(chan *pb.DecryptRequest)}
	timeout := 20 * time.Millisecond
	session := &grpcDecryptSession{
		stream: fake, cancel: cancel, adamID: "adam", fragmentTimeout: timeout,
	}

	first, err := session.DecryptFragment("key", [][]byte{{1}})
	if err != nil || len(first) != 1 || first[0][0] != 1 {
		t.Fatalf("first fragment = (%v, %v)", first, err)
	}
	// An old whole-session deadline would expire here. A fresh fragment gets
	// its own inactivity window and must still succeed.
	time.Sleep(2 * timeout)
	second, err := session.DecryptFragment("key", [][]byte{{2}})
	if err != nil || len(second) != 1 || second[0][0] != 2 {
		t.Fatalf("second fragment after idle gap = (%v, %v)", second, err)
	}
}

// scriptedDecryptStream accepts every Send and answers Recv from a fixed reply
// list, then reports io.EOF.
type scriptedDecryptStream struct {
	ctx     context.Context
	replies []*pb.DecryptReply
	sends   atomic.Int32
}

func (f *scriptedDecryptStream) Send(*pb.DecryptRequest) error {
	f.sends.Add(1)
	return nil
}
func (f *scriptedDecryptStream) Recv() (*pb.DecryptReply, error) {
	if len(f.replies) == 0 {
		return nil, io.EOF
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	return reply, nil
}
func (f *scriptedDecryptStream) Header() (metadata.MD, error) { return nil, nil }
func (f *scriptedDecryptStream) Trailer() metadata.MD         { return nil }
func (f *scriptedDecryptStream) CloseSend() error             { return nil }
func (f *scriptedDecryptStream) Context() context.Context     { return f.ctx }
func (f *scriptedDecryptStream) SendMsg(any) error            { return nil }
func (f *scriptedDecryptStream) RecvMsg(any) error            { return nil }

func decryptReply(index int32, sample []byte) *pb.DecryptReply {
	return &pb.DecryptReply{Header: &pb.ReplyHeader{Code: replyOK}, Data: &pb.DecryptData{SampleIndex: index, Sample: sample}}
}

func TestDecryptFragmentPlacesRepliesByIndex(t *testing.T) {
	tests := []struct {
		name    string
		replies []*pb.DecryptReply
		want    [][]byte
		wantErr string
	}{
		{
			name:    "out-of-order replies land in their slots",
			replies: []*pb.DecryptReply{decryptReply(1, []byte("b")), decryptReply(0, []byte("a"))},
			want:    [][]byte{[]byte("a"), []byte("b")},
		},
		{
			// An empty sample decodes as a nil slice; it still fills its slot.
			name:    "empty sample counts as received",
			replies: []*pb.DecryptReply{decryptReply(0, nil), decryptReply(1, []byte("b"))},
			want:    [][]byte{nil, []byte("b")},
		},
		{
			name:    "duplicate index is rejected",
			replies: []*pb.DecryptReply{decryptReply(0, []byte("a")), decryptReply(0, []byte("a"))},
			wantErr: "sample index 0 twice",
		},
		{
			name:    "index outside the batch is rejected",
			replies: []*pb.DecryptReply{decryptReply(0, []byte("a")), decryptReply(2, []byte("c"))},
			wantErr: "out-of-range sample index 2",
		},
		{
			name:    "stream ending early is reported",
			replies: []*pb.DecryptReply{decryptReply(0, []byte("a"))},
			wantErr: "ended after 1/2 samples",
		},
		{
			name:    "error reply is reported",
			replies: []*pb.DecryptReply{{Header: &pb.ReplyHeader{Code: replyFailed, Msg: "missing decrypt data"}}},
			wantErr: "missing decrypt data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream := &scriptedDecryptStream{ctx: ctx, replies: tt.replies}
			session := &grpcDecryptSession{stream: stream, cancel: cancel, adamID: "adam"}

			got, err := session.DecryptFragment("key", [][]byte{[]byte("x"), []byte("y")})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("plaintext = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecryptFragmentRejectsIndexesBeyondInt32(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &scriptedDecryptStream{ctx: ctx}
	// The next sample would be math.MaxInt32+1, which the proto's int32
	// sample_index cannot carry.
	session := &grpcDecryptSession{stream: stream, cancel: cancel, adamID: "adam", next: math.MaxInt32}

	if _, err := session.DecryptFragment("key", [][]byte{{1}, {2}}); err == nil || !strings.Contains(err.Error(), "int32") {
		t.Fatalf("error = %v, want an int32 range error", err)
	}
	if got := stream.sends.Load(); got != 0 {
		t.Fatalf("sent %d samples with indexes that would wrap, want 0", got)
	}
}
