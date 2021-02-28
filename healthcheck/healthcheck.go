package main

import (
	"os"
	
	"github.com/go-redis/redis/v7"
)

func main() {
	host := os.Getenv("LISTEN")
	if len(host) == 0 {
		host = "localhost:9999"
	}
	pass := os.Getenv("PASSWORD")
	client := redis.NewClient(&redis.Options{ Addr: host, Password: pass, })
	role, err := client.Do("role").Result()
	if err != nil {
		os.Exit(1)
	}
	status := role.([]interface{})
	currentRole := status[0]
	if currentRole == "master" {
		os.Exit(0)
	}
	if currentRole == "slave" && status[3] == "connected" {
		os.Exit(0)
	}
	os.Exit(127)
}
