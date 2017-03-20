package ladon

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
	"gopkg.in/redis.v5"

	"github.com/ory/ladon/manager"
	"github.com/ory/ladon/policy"
)

// RedisManager is a redis implementation of Manager to store policies persistently.
type RedisManager struct {
	db        *redis.Client
	keyPrefix string
}

// NewManager initializes a new RedisManager with no policies
func NewManager(opts ...manager.Option) (manager.Manager, error) {
	var o manager.Options
	for _, opt := range opts {
		opt(&o)
	}

	// Check if a db was already initialized and stored
	if conn, ok := o.GetConnection(); ok {
		if db, ok := conn.(*redis.Client); ok {
			return &RedisManager{
				db:        db,
				keyPrefix: o.PolicyTable,
			}, nil
		}
	}

	// TODO: Create new redis client connection
	return nil, nil
}

func init() {
	manager.DefaultManagers["redis"] = NewManager
}

const redisPolicies = "ladon:policies"

var (
	redisPolicyExists = errors.New("Policy exists")
	redisNotFound     = errors.New("Not found")
)

func (m *RedisManager) redisPoliciesKey() string {
	return fmt.Sprintf("%s:%s", m.keyPrefix, redisPolicies)
}

// Create a new policy to RedisManager
func (m *RedisManager) Create(policy policy.Policy) error {
	payload, err := json.Marshal(policy)
	if err != nil {
		return errors.Wrap(err, "policy marshal failed")
	}

	wasKeySet, err := m.db.HSetNX(m.redisPoliciesKey(), policy.GetID(), string(payload)).Result()
	if !wasKeySet {
		return errors.WithStack(redisPolicyExists)
	} else if err != nil {
		return errors.Wrap(err, "policy creation failed")
	}

	return nil
}

// Get retrieves a policy.
func (m *RedisManager) Get(id string) (policy.Policy, error) {
	resp, err := m.db.HGet(m.redisPoliciesKey(), id).Bytes()
	if err == redis.Nil {
		return nil, redisNotFound
	} else if err != nil {
		return nil, errors.WithStack(err)
	}
	return redisUnmarshalPolicy(resp)
}

// Delete removes a policy.
func (m *RedisManager) Delete(id string) error {
	if err := m.db.HDel(m.redisPoliciesKey(), id).Err(); err != nil {
		return errors.Wrap(err, "policy deletion failed")
	}
	return nil
}

// FindPoliciesForSubject finds all policies associated with the subject.
func (m *RedisManager) FindPoliciesForSubject(subject string) (policy.Policies, error) {
	var ps policy.Policies

	iter := m.db.HScan(m.redisPoliciesKey(), 0, "", 0).Iterator()
	for iter.Next() {
		if !iter.Next() {
			break
		}
		resp := []byte(iter.Val())

		p, err := redisUnmarshalPolicy(resp)
		if err != nil {
			return nil, err
		}

		if ok, err := policy.Match(p, p.GetSubjects(), subject); err != nil {
			return nil, errors.Wrap(err, "policy subject match failed")
		} else if !ok {
			continue
		}

		ps = append(ps, p)
	}
	if err := iter.Err(); err != nil {
		return nil, errors.WithStack(err)
	}

	return ps, nil
}

func redisUnmarshalPolicy(pol []byte) (policy.Policy, error) {
	var p policy.DefaultPolicy
	if err := json.Unmarshal(pol, &p); err != nil {
		return nil, errors.Wrap(err, "policy unmarshal failed")
	}

	return &p, nil
}