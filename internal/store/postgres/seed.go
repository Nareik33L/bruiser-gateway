package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
)

type Merchant struct {
	ID            string
	Name          string
	IDPHMACSecret string
}

type SessionRow struct {
	ID             string
	MerchantID     string
	CustomerID     string
	Anchors        map[string]string
	PrincipalType  string
	PrincipalID    string
	ExpiresAt      time.Time
}

type SigningKey struct {
	KID        string
	MerchantID string
	Public     ed25519.PublicKey
	Private    ed25519.PrivateKey
}

func (s *Store) EnsureMerchant(ctx context.Context, merchantID, name, hmacSecret string) error {
	_, err := s.pool.Exec(ctx, `
		insert into merchants (merchant_id, name, idp_hmac_secret)
		values ($1, $2, $3)
		on conflict (merchant_id) do update
		  set name = excluded.name,
		      idp_hmac_secret = coalesce(excluded.idp_hmac_secret, merchants.idp_hmac_secret)`,
		merchantID, name, hmacSecret)
	return wrapStore(err)
}

func (s *Store) Merchant(ctx context.Context, merchantID string) (Merchant, error) {
	var m Merchant
	err := s.pool.QueryRow(ctx, `
		select merchant_id, name, coalesce(idp_hmac_secret, '')
		from merchants where merchant_id = $1`, merchantID,
	).Scan(&m.ID, &m.Name, &m.IDPHMACSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, err
	}
	return m, wrapStore(err)
}

func (s *Store) EnsureSigningKey(ctx context.Context, merchantID string) (SigningKey, error) {
	var k SigningKey
	var pub, priv []byte
	err := s.pool.QueryRow(ctx, `
		select kid, merchant_id, public_key, private_key
		from signing_keys
		where merchant_id = $1 and retired_at is null
		order by created_at desc
		limit 1`, merchantID).Scan(&k.KID, &k.MerchantID, &pub, &priv)
	if err == nil {
		k.Public = ed25519.PublicKey(pub)
		k.Private = ed25519.PrivateKey(priv)
		return k, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return k, wrapStore(err)
	}
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return k, err
	}
	k = SigningKey{
		KID:        id.Key(),
		MerchantID: merchantID,
		Public:     pubKey,
		Private:    privKey,
	}
	_, err = s.pool.Exec(ctx, `
		insert into signing_keys (kid, merchant_id, public_key, private_key)
		values ($1, $2, $3, $4)`, k.KID, merchantID, []byte(pubKey), []byte(privKey))
	if err != nil {
		return k, wrapStore(err)
	}
	return k, nil
}

func (s *Store) ActiveSigningKeys(ctx context.Context, merchantID string) ([]SigningKey, error) {
	rows, err := s.pool.Query(ctx, `
		select kid, merchant_id, public_key, private_key
		from signing_keys
		where merchant_id = $1 and retired_at is null
		order by created_at desc`, merchantID)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	var out []SigningKey
	for rows.Next() {
		var k SigningKey
		var pub, priv []byte
		if err := rows.Scan(&k.KID, &k.MerchantID, &pub, &priv); err != nil {
			return nil, wrapStore(err)
		}
		k.Public = ed25519.PublicKey(pub)
		k.Private = ed25519.PrivateKey(priv)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) InsertSession(ctx context.Context, sess SessionRow) error {
	anchors, err := json.Marshal(sess.Anchors)
	if err != nil {
		return err
	}
	if sess.Anchors == nil {
		anchors = []byte("{}")
	}
	_, err = s.pool.Exec(ctx, `
		insert into sessions (
			session_id, merchant_id, customer_id, anchors,
			principal_type, principal_id, expires_at
		) values ($1,$2,$3,$4,$5,$6,$7)`,
		sess.ID, sess.MerchantID, sess.CustomerID, anchors,
		sess.PrincipalType, sess.PrincipalID, sess.ExpiresAt)
	return wrapStore(err)
}

func (s *Store) Session(ctx context.Context, sessionID string) (SessionRow, error) {
	var sess SessionRow
	var anchors []byte
	err := s.pool.QueryRow(ctx, `
		select session_id, merchant_id, customer_id, anchors,
		       principal_type, principal_id, expires_at
		from sessions where session_id = $1`, sessionID,
	).Scan(&sess.ID, &sess.MerchantID, &sess.CustomerID, &anchors,
		&sess.PrincipalType, &sess.PrincipalID, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, err
	}
	if err != nil {
		return sess, wrapStore(err)
	}
	if len(anchors) > 0 {
		_ = json.Unmarshal(anchors, &sess.Anchors)
	}
	if sess.Anchors == nil {
		sess.Anchors = map[string]string{}
	}
	return sess, nil
}
