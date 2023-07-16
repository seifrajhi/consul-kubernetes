package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-redis/redis"
	socketio "github.com/googollee/go-socket.io"
	consulapi "github.com/hashicorp/consul/api"
)

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func getConsul(address string) (*consulapi.Client, error) {
	config := consulapi.DefaultConfig()
	config.Address = address
	consul, err := consulapi.NewClient(config)
	return consul, err

}

func getKvPair(client *consulapi.Client, key string) (*consulapi.KVPair, error) {
	kv := client.KV()
	keyPair, _, err := kv.Get(key, nil)
	return keyPair, err
}

func main() {
	consulAddress := getEnv("CONSUL_HTTP_ADDR", "")
	if consulAddress == "" {
		log.Fatalf("CONSUL_HTTP_ADDRESS environment variable not set")
	}

	consul, err := getConsul(consulAddress)
	if err != nil {
		log.Fatalf("Error connecting to Consul: %s", err)
	}

	cat := consul.Catalog()
	svc, _, err := cat.Service("redissvc-default", "", nil)
	log.Printf("Service address and port: %s:%d\n", svc[0].ServiceAddress, svc[0].ServicePort)

	redisPattern, err := getKvPair(consul, "REDISPATTERN")
	if err != nil || redisPattern == nil {
		log.Fatalf("Could not get REDISPATTERN: %s", err)
	}
	log.Printf("KV: %v %s\n", redisPattern.Key, redisPattern.Value)

	// redis connection
	client := redis.NewClient(&redis.Options{
		Addr: getEnv("REDISHOST", "localhost:6379"),
	})

	// subscribe to all channels
	pubsub := client.PSubscribe(string(redisPattern.Value))

	_, err = pubsub.Receive()
	if err != nil {
		panic(err)
	}

	// messages received on a Go channel
	ch := pubsub.Channel()

	// ping Redis server
	pong, err := client.Ping().Result()
	log.Println(pong, err)

	server := getWSServer("channel")
	if server == nil {
		log.Fatalln("Could not create WebSockets server")
	}

	// Consume messages from Redis
	go func(srv *socketio.Server) {
		for msg := range ch {
			log.Println(msg.Channel, msg.Payload)
			srv.BroadcastTo(msg.Channel, "message", msg.Payload)
		}
	}(server)

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	//mux.Handle("/", http.FileServer(http.Dir("./assets")))
	mux.HandleFunc("/", handle)

	http.ListenAndServe(":8080", mux)

}

func handle(w http.ResponseWriter, req *http.Request) {

	http.ServeFile(w, req, "./assets")

}
