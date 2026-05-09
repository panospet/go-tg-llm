package conversation

import (
	"context"
	"time"

	"github.com/jellydator/ttlcache/v3"

	"go-tg-llm/internal/llm"
)

const (
	// DefaultTTL is how long a conversation is kept since its last update.
	DefaultTTL = 5 * 24 * time.Hour
	// CleanupInterval is how often expired entries are purged.
	CleanupInterval = 24 * time.Hour
	// DefaultMaxTurns is the maximum number of messages retained per thread.
	DefaultMaxTurns = 20
)

// convKey uniquely identifies a conversation thread within a Telegram chat.
type convKey struct {
	ChatID    int64
	MessageID int // the bot message ID that was replied to
}

// Conversation holds the full message history for a single thread and the
// name of the provider that started it.
type Conversation struct {
	Messages []llm.Message
	Provider string
}

// Store wraps a ttlcache.Cache to persist per-thread conversation history,
// keyed by (chatID, botMessageID) with a 5-day TTL per entry.
type Store struct {
	cache    *ttlcache.Cache[convKey, *Conversation]
	maxTurns int
}

// NewStore creates a Store. Call StartCleanup to enable daily eviction.
func NewStore(maxTurns int) *Store {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	c := ttlcache.New[convKey, *Conversation](
		ttlcache.WithTTL[convKey, *Conversation](DefaultTTL),
	)
	return &Store{cache: c, maxTurns: maxTurns}
}

// Get retrieves the conversation for a given bot message ID.
// Returns nil, false when not found or expired.
func (s *Store) Get(chatID int64, botMsgID int) (*Conversation, bool) {
	item := s.cache.Get(convKey{ChatID: chatID, MessageID: botMsgID})
	if item == nil {
		return nil, false
	}
	return item.Value(), true
}

// Save stores a conversation under botMsgID, (re)setting the 5-day TTL.
// If the message slice exceeds maxTurns, the oldest messages are trimmed.
func (s *Store) Save(chatID int64, botMsgID int, conv *Conversation) {
	if len(conv.Messages) > s.maxTurns {
		conv.Messages = conv.Messages[len(conv.Messages)-s.maxTurns:]
	}
	s.cache.Set(convKey{ChatID: chatID, MessageID: botMsgID}, conv, ttlcache.DefaultTTL)
}

// StartCleanup runs a blocking loop that calls DeleteExpired once per day.
// Cancel ctx to stop it. Meant to be run in a goroutine.
func (s *Store) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cache.DeleteExpired()
		case <-ctx.Done():
			return
		}
	}
}
