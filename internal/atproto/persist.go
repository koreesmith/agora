package atproto

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"

	"github.com/agora-social/agora/internal/store"
)

// pgEventPersister implements indigo's events.EventPersistence against
// Postgres — the durable, replayable log a subscribeRepos client's cursor
// reconnects against (AGORA-191's "resume from the right point, not replay
// everything or drop events" requirement). Indigo ships disk/Pebble/GORM
// persister options; none fit "everything lives in Postgres, no new storage
// system," so this is a small direct implementation instead.
type pgEventPersister struct {
	db        *store.DB
	broadcast func(*events.XRPCStreamEvent)
}

func newPgEventPersister(db *store.DB) *pgEventPersister {
	return &pgEventPersister{db: db}
}

// Persist assigns the next sequence number, serializes the event with that
// seq already baked into its payload (so a replayed event round-trips with
// the same seq the original subscriber saw it as), and durably stores it
// before broadcasting to live subscribers. This satisfies indigo's
// EventPersistence interface; commit events instead go through persistTx so
// they share a transaction with the repo-head update (see repo.go's
// commitAndPersist), but keeping this correct guards any other AddEvent path.
func (p *pgEventPersister) Persist(ctx context.Context, e *events.XRPCStreamEvent) error {
	var seq int64
	if err := p.db.QueryRowContext(ctx, `SELECT nextval('atproto_firehose_seq')`).Scan(&seq); err != nil {
		return fmt.Errorf("reserving firehose seq: %w", err)
	}
	if err := setEventSeq(e, seq); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := e.Serialize(&buf); err != nil {
		return fmt.Errorf("serializing event: %w", err)
	}
	if _, err := p.db.ExecContext(ctx, `
		INSERT INTO atproto_firehose_events (seq, data) VALUES ($1, $2)
	`, seq, buf.Bytes()); err != nil {
		return fmt.Errorf("persisting event: %w", err)
	}

	p.broadcastEvent(e)
	return nil
}

// persistTx assigns the next sequence number and durably appends the event
// within the caller's transaction, WITHOUT broadcasting — the piece
// commitAndPersist runs in the same transaction as the repo-head update so
// the stored head and the firehose log can never disagree. Broadcasting is
// deferred to broadcastEvent, called only after that transaction commits, so
// a live subscriber never sees an event that isn't yet durable.
func (p *pgEventPersister) persistTx(ctx context.Context, tx *sql.Tx, e *events.XRPCStreamEvent) error {
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('atproto_firehose_seq')`).Scan(&seq); err != nil {
		return fmt.Errorf("reserving firehose seq: %w", err)
	}
	if err := setEventSeq(e, seq); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := e.Serialize(&buf); err != nil {
		return fmt.Errorf("serializing event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO atproto_firehose_events (seq, data) VALUES ($1, $2)
	`, seq, buf.Bytes()); err != nil {
		return fmt.Errorf("persisting event: %w", err)
	}
	return nil
}

// broadcastEvent hands a persisted event to live subscribers (the callback
// indigo's EventManager wired in via SetEventBroadcaster).
func (p *pgEventPersister) broadcastEvent(e *events.XRPCStreamEvent) {
	if p.broadcast != nil {
		p.broadcast(e)
	}
}

// setEventSeq stamps the reserved sequence number onto whichever event
// variant is set, so the seq is baked into the serialized payload a
// reconnecting subscriber replays.
func setEventSeq(e *events.XRPCStreamEvent, seq int64) error {
	switch {
	case e.RepoCommit != nil:
		e.RepoCommit.Seq = seq
	case e.RepoSync != nil:
		e.RepoSync.Seq = seq
	case e.RepoIdentity != nil:
		e.RepoIdentity.Seq = seq
	case e.RepoAccount != nil:
		e.RepoAccount.Seq = seq
	case e.LabelLabels != nil:
		e.LabelLabels.Seq = seq
	default:
		return fmt.Errorf("no event set on XRPCStreamEvent")
	}
	return nil
}

// Playback replays every event after (not including) since, in seq order —
// what a reconnecting subscriber's cursor drives.
func (p *pgEventPersister) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	rows, err := p.db.QueryContext(ctx, `
		SELECT data FROM atproto_firehose_events WHERE seq > $1 ORDER BY seq ASC
	`, since)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return err
		}
		var evt events.XRPCStreamEvent
		if err := evt.Deserialize(bytes.NewReader(data)); err != nil {
			return fmt.Errorf("deserializing persisted event: %w", err)
		}
		if err := cb(&evt); err != nil {
			return err
		}
	}
	return rows.Err()
}

// TakeDownRepo (repo moderation takedowns) isn't implemented yet — no Agora
// feature calls this path today. Matches indigo's own MemPersister, which
// errors here too rather than silently no-op'ing.
func (p *pgEventPersister) TakeDownRepo(ctx context.Context, uid models.Uid) error {
	return fmt.Errorf("repo takedowns not implemented")
}

func (p *pgEventPersister) Flush(ctx context.Context) error    { return nil }
func (p *pgEventPersister) Shutdown(ctx context.Context) error { return nil }

func (p *pgEventPersister) SetEventBroadcaster(cb func(*events.XRPCStreamEvent)) {
	p.broadcast = cb
}
