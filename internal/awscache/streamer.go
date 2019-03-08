package awscache

import (
	"context"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/pkg/errors"
	"sync"
	"time"
)

// StackStreamer sends cloudformation events about a stack into stdout
type StackStreamer struct {
	PollInterval time.Duration
	once         sync.Once
	closeOnDone  chan struct{}
}

func (s *StackStreamer) pollInterval() time.Duration {
	if s.PollInterval == 0 {
		return time.Second
	}
	return s.PollInterval
}

func (s *StackStreamer) init() {
	s.closeOnDone = make(chan struct{})
}

// Start streaming clouformation events
func (s *StackStreamer) Start(ctx context.Context, clients *AWSClients, stackID string, streamInto chan<- *cloudformation.StackEvent) error {
	s.once.Do(s.init)
	cloudformationClient := cloudformation.New(clients.session)
	return s.streamStackEvents(ctx, cloudformationClient, stackID, clients.token(), streamInto)
}

// Close stops streaming cloudformation events
func (s *StackStreamer) Close() error {
	s.once.Do(s.init)
	close(s.closeOnDone)
	return nil
}

// streamStackEvents sends cloudformation events into a channel until told to stop.
func (s *StackStreamer) streamStackEvents(ctx context.Context, cloudformationClient *cloudformation.CloudFormation, stackID string, clientRequestToken string, streamInto chan<- *cloudformation.StackEvent) error {
	var stopEventID string
	for {
		select {
		case <-s.closeOnDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.pollInterval()):
		}

		// All the events come (most recent first), so we have to fetch them, then stream them backwards into
		// the chan
		newEvents, err := s.retEvents(ctx, cloudformationClient, stackID, clientRequestToken, stopEventID)
		if err != nil {
			return errors.Wrap(err, "unable to fetch recent events")
		}
		for i := len(newEvents) - 1; i >= 0; i-- {
			stopEventID = emptyOnNil(newEvents[i].EventId)
			// (Once we've seen a single event with our client request token, stream ALL events)
			// This lets us see cancel events
			clientRequestToken = ""
			select {
			case <-s.closeOnDone:
				return nil
			case streamInto <- newEvents[i]:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *StackStreamer) retEvents(ctx context.Context, cloudformationClient *cloudformation.CloudFormation, stackID string, clientRequestToken string, stopEventID string) ([]*cloudformation.StackEvent, error) {
	var nextToken *string
	var ret []*cloudformation.StackEvent
	for {
		descOut, err := cloudformationClient.DescribeStackEventsWithContext(ctx, &cloudformation.DescribeStackEventsInput{
			StackName: &stackID,
			NextToken: nextToken,
		})
		if err != nil {
			return nil, errors.Wrap(err, "unable to describe stack events")
		}
		for _, event := range descOut.StackEvents {
			// The event has to be for this stack
			if emptyOnNil(event.EventId) == stopEventID {
				return ret, nil
			}
			if clientRequestToken != "" && emptyOnNil(event.ClientRequestToken) != clientRequestToken {
				return ret, nil
			}
			ret = append(ret, event)
		}
	}
}

//
//func prettyEvent(event *cloudformation.StackEvent) string {
//	ret := struct {
//		LogicalResourceID    string `json:",omitempty"`
//		PhysicalResourceID   string `json:",omitempty"`
//		ResourceStatus       string `json:",omitempty"`
//		ResourceStatusReason string `json:",omitempty"`
//		ResourceType         string `json:",omitempty"`
//	}{
//		emptyOnNil(event.LogicalResourceId),
//		emptyOnNil(event.PhysicalResourceId),
//		emptyOnNil(event.ResourceStatus),
//		emptyOnNil(event.ResourceStatusReason),
//		emptyOnNil(event.ResourceType),
//	}
//	return awsutil.Prettify(ret)
//}
