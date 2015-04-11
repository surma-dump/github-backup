package common

import (
	"fmt"
	"net/url"

	"github.com/garyburd/redigo/redis"
)

func ConnectRedis(s string) (redis.Conn, error) {
	redisUrl, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("Could not parse redis url: %s", err)
	}
	if redisUrl.Scheme != "redis" {
		return nil, fmt.Errorf("Unsupported redis scheme %s", redisUrl.Scheme)
	}

	conn, err := redis.Dial("tcp", redisUrl.Host)
	if err != nil {
		return conn, err
	}
	if redisUrl.User != nil {
		pass, ok := redisUrl.User.Password()
		if !ok {
			pass = redisUrl.User.Username()
		}
		_, err := conn.Do("AUTH", pass)
		if err != nil {
			return conn, err
		}
	}
	_, err = conn.Do("EXISTS", "github-backup:lastrun")
	return conn, err
}
