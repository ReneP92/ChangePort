package mail

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// SES sends through Amazon SES's API, in the AWS environments, with the
// credentials of the pod's EKS Pod Identity.
type SES struct {
	Client *sesv2.Client
}

var _ Sender = SES{}

// Send implements Sender.
func (s SES) Send(ctx context.Context, from string, message Message) error {
	content := func(data string) *types.Content {
		return &types.Content{Data: aws.String(data), Charset: aws.String("UTF-8")}
	}
	_, err := s.Client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(from),
		Destination:      &types.Destination{ToAddresses: message.To},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: content(message.Subject),
			Body:    &types.Body{Text: content(message.Text), Html: content(message.HTML)},
		}},
	})
	if err != nil {
		return fmt.Errorf("send through SES: %w", err)
	}
	return nil
}
