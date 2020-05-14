// Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
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

// Package mqtt implements the MQTT provider using the mqtt driver.
package mqtt

import (
	"context"
	"net/http"
	"net/url"
	"time"

	mqtt_topic "github.com/TheThingsIndustries/mystique/pkg/topic"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.thethings.network/lorawan-stack/v3/pkg/applicationserver/io/pubsub/provider"
	"go.thethings.network/lorawan-stack/v3/pkg/errors"
	"go.thethings.network/lorawan-stack/v3/pkg/ttnpb"
	"gocloud.dev/pubsub"
)

var timeout = (1 << 3) * time.Second

type impl struct {
}

type connection struct {
	mqtt.Client
}

// Shutdown implements provider.Shutdowner.
func (c *connection) Shutdown(_ context.Context) error {
	c.Disconnect(uint(timeout / time.Millisecond))
	return nil
}

var errConnectFailed = errors.Define("connect_failed", "connection to MQTT server failed")

// OpenConnection implements provider.Provider using the mqtt driver.
func (impl) OpenConnection(ctx context.Context, target provider.Target) (pc *provider.Connection, err error) {
	providerI := target.GetProvider()
	if settings, ok := providerI.(*ttnpb.ApplicationPubSub_AWSIoT); ok {
		providerI, err = awsIotMQTTProvider(ctx, settings)
		if err != nil {
			return nil, err
		}
	}
	settings, ok := providerI.(*ttnpb.ApplicationPubSub_MQTT)
	if !ok {
		panic("wrong provider type provided to OpenConnection")
	}
	serverURL, err := adaptURLScheme(settings.MQTT.ServerURL)
	if err != nil {
		return nil, err
	}
	clientOpts := mqtt.NewClientOptions()
	clientOpts.AddBroker(serverURL)
	clientOpts.SetClientID(settings.MQTT.ClientID)
	clientOpts.SetUsername(settings.MQTT.Username)
	clientOpts.SetPassword(settings.MQTT.Password)
	if settings.MQTT.UseTLS {
		config, err := createTLSConfig(settings.MQTT.TLSCA, settings.MQTT.TLSClientCert, settings.MQTT.TLSClientKey)
		if err != nil {
			return nil, err
		}
		clientOpts.SetTLSConfig(config)
	}
	if len(settings.MQTT.Headers) > 0 {
		headers := make(http.Header, len(settings.MQTT.Headers))
		for k, v := range settings.MQTT.Headers {
			headers.Set(k, v)
		}
		clientOpts.SetHTTPHeaders(headers)
	}
	client := mqtt.NewClient(clientOpts)
	token := client.Connect()
	if !token.WaitTimeout(timeout) {
		return nil, errConnectFailed.WithCause(context.DeadlineExceeded)
	} else if token.Error() != nil {
		return nil, errConnectFailed.WithCause(token.Error())
	}
	pc = &provider.Connection{
		ProviderConnection: &connection{
			Client: client,
		},
	}
	for _, t := range []struct {
		topic   **pubsub.Topic
		message *ttnpb.ApplicationPubSub_Message
	}{
		{
			topic:   &pc.Topics.UplinkMessage,
			message: target.GetUplinkMessage(),
		},
		{
			topic:   &pc.Topics.JoinAccept,
			message: target.GetJoinAccept(),
		},
		{
			topic:   &pc.Topics.DownlinkAck,
			message: target.GetDownlinkAck(),
		},
		{
			topic:   &pc.Topics.DownlinkNack,
			message: target.GetDownlinkNack(),
		},
		{
			topic:   &pc.Topics.DownlinkSent,
			message: target.GetDownlinkSent(),
		},
		{
			topic:   &pc.Topics.DownlinkFailed,
			message: target.GetDownlinkFailed(),
		},
		{
			topic:   &pc.Topics.DownlinkQueued,
			message: target.GetDownlinkQueued(),
		},
		{
			topic:   &pc.Topics.LocationSolved,
			message: target.GetLocationSolved(),
		},
	} {
		if t.message == nil {
			continue
		}
		if *t.topic, err = OpenTopic(
			client,
			mqtt_topic.Join(append(mqtt_topic.Split(target.GetBaseTopic()), mqtt_topic.Split(t.message.GetTopic())...)),
			timeout,
			byte(settings.MQTT.PublishQoS),
		); err != nil {
			client.Disconnect(uint(timeout / time.Millisecond))
			return nil, err
		}
	}
	for _, s := range []struct {
		subscription **pubsub.Subscription
		message      *ttnpb.ApplicationPubSub_Message
	}{
		{
			subscription: &pc.Subscriptions.Push,
			message:      target.GetDownlinkPush(),
		},
		{
			subscription: &pc.Subscriptions.Replace,
			message:      target.GetDownlinkReplace(),
		},
	} {
		if s.message == nil {
			continue
		}
		if *s.subscription, err = OpenSubscription(
			client,
			mqtt_topic.Join(append(mqtt_topic.Split(target.GetBaseTopic()), mqtt_topic.Split(s.message.GetTopic())...)),
			timeout,
			byte(settings.MQTT.SubscribeQoS),
		); err != nil {
			client.Disconnect(uint(timeout / time.Millisecond))
			return nil, err
		}
	}
	return pc, nil
}

func adaptURLScheme(initial string) (string, error) {
	u, err := url.Parse(initial)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "mqtt":
		u.Scheme = "tcp"
	case "mqtts":
		u.Scheme = "ssl"
	}
	return u.String(), nil
}

func init() {
	provider.RegisterProvider(&ttnpb.ApplicationPubSub_MQTT{}, impl{})
}
