CREATE TABLE mzfw_messagebus_messages (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  idempotency_key TEXT NOT NULL UNIQUE,
  message_json TEXT NOT NULL,
  ack_json TEXT NOT NULL
);
