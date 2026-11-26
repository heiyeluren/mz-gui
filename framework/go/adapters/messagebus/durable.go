package messagebus

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

type DurableOptions struct {
	Options      Options
	DatabasePath string
}

type DurableBus struct {
	bus      *Bus
	database *sql.DB
}

var _ contract.MessageBus = (*DurableBus)(nil)

//go:embed migrations
var migrationFS embed.FS

func NewDurable(options DurableOptions) (*DurableBus, error) {
	if options.DatabasePath == "" {
		return nil, contract.ErrInvalidContract
	}
	database, err := sqlite.Open(context.Background(), sqlite.Options{
		Path: options.DatabasePath,
		Bundles: []sqlite.MigrationBundle{{
			Owner:      "framework.messagebus",
			Migrations: migrationFS, MigrationDir: "migrations",
			Objects: []sqlite.SchemaObject{{Type: sqlite.SchemaObjectTable, Name: "mzfw_messagebus_messages"}},
		}},
	})
	if err != nil {
		return nil, err
	}
	bus, err := New(options.Options)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	durable := &DurableBus{bus: bus, database: database}
	if err := durable.load(); err != nil {
		_ = bus.Close(context.Background())
		_ = database.Close()
		return nil, err
	}
	return durable, nil
}

func (durable *DurableBus) load() error {
	rows, err := durable.database.Query("SELECT message_json FROM mzfw_messagebus_messages ORDER BY sequence")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return err
		}
		var message contract.Message
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			return err
		}
		if _, err := durable.bus.Publish(context.Background(), message); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (durable *DurableBus) Publish(ctx context.Context, message contract.Message) (contract.PublishAck, error) {
	key := string(message.Meta.IdempotencyKey)
	var existing string
	if err := durable.database.QueryRowContext(ctx, "SELECT ack_json FROM mzfw_messagebus_messages WHERE idempotency_key = ?", key).Scan(&existing); err == nil {
		var ack contract.PublishAck
		if err := json.Unmarshal([]byte(existing), &ack); err != nil {
			return contract.PublishAck{}, err
		}
		return ack, nil
	} else if err != sql.ErrNoRows {
		return contract.PublishAck{}, err
	}
	ack, err := durable.bus.Publish(ctx, message)
	if err != nil {
		return contract.PublishAck{}, err
	}
	messageData, err := json.Marshal(message)
	if err != nil {
		return contract.PublishAck{}, err
	}
	ackData, err := json.Marshal(ack)
	if err != nil {
		return contract.PublishAck{}, err
	}
	if _, err := durable.database.ExecContext(ctx, "INSERT INTO mzfw_messagebus_messages(idempotency_key, message_json, ack_json) VALUES (?, ?, ?)", key, messageData, ackData); err != nil {
		return contract.PublishAck{}, err
	}
	return ack, nil
}

func (durable *DurableBus) Subscribe(ctx context.Context, request contract.SubscriptionRequest) (contract.MessageStream, error) {
	return durable.bus.Subscribe(ctx, request)
}

func (durable *DurableBus) Ack(ctx context.Context, deliveryID contract.DeliveryID) error {
	return durable.bus.Ack(ctx, deliveryID)
}

func (durable *DurableBus) Nack(ctx context.Context, request contract.NackRequest) error {
	return durable.bus.Nack(ctx, request)
}

func (durable *DurableBus) Info(ctx context.Context) (contract.AdapterInfo, error) {
	return durable.bus.Info(ctx)
}

func (durable *DurableBus) Health(ctx context.Context) (health.Report, error) {
	return durable.bus.Health(ctx)
}

func (durable *DurableBus) Close(ctx context.Context) error {
	if durable == nil {
		return contract.ErrInvalidContract
	}
	if err := durable.bus.Close(ctx); err != nil {
		return err
	}
	return durable.database.Close()
}
