package common

import (
	"fmt"
	"net/url"
	"time"

	"github.com/garyburd/redigo/redis"
)

// CheckRedis checks if the given URL is valid.
// This includes checking for valid authentication.
func CheckRedis(s string) error {
	redisURL, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("Could not parse redis url: %s", err)
	}
	if redisURL.Scheme != "redis" {
		return fmt.Errorf("Unsupported redis scheme %s", redisURL.Scheme)
	}

	conn, err := redis.Dial("tcp", redisURL.Host)
	if err != nil {
		return err
	}
	if redisURL.User != nil {
		pass, ok := redisURL.User.Password()
		if !ok {
			pass = redisURL.User.Username()
		}
		_, err := conn.Do("AUTH", pass)
		if err != nil {
			return err
		}
	}
	_, err = conn.Do("EXISTS", "somekey")
	return err
}

// CreateRedisPool creates a new pool for a given Redis URL>
// The CheckRedis is expected to return nil when given the same
// parameter.
func CreateRedisPool(s string) *redis.Pool {
	redisURL, _ := url.Parse(s)

	return &redis.Pool{
		Dial: func() (redis.Conn, error) {
			conn, err := redis.Dial("tcp", redisURL.Host)
			if err != nil {
				return nil, err
			}
			if redisURL.User != nil {
				pass, ok := redisURL.User.Password()
				if !ok {
					pass = redisURL.User.Username()
				}
				_, err := conn.Do("AUTH", pass)
				if err != nil {
					return nil, err
				}
			}
			return conn, err
		},
		MaxIdle:     3,
		IdleTimeout: 1 * time.Minute,
	}
}
