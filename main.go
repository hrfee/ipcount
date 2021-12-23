package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	"gopkg.in/ini.v1"
)

const (
	NAME = "ipcount"
)

type Threshold struct {
	duration time.Duration
}

func NewThreshold(days, hours, minutes int) *Threshold {
	return &Threshold{(time.Duration((days*24)+hours) * time.Hour) + (time.Duration(minutes) * time.Minute)}
}

func (threshold *Threshold) Valid(t time.Time) bool {
	return t.Add(threshold.duration).After(time.Now())
}

type Hash interface {
	Hash(string) []byte
}

type HMACSha256 struct {
	h hash.Hash
}

func (h *HMACSha256) Hash(in string) []byte {
	h.h.Reset()
	h.h.Write([]byte(in))
	return h.h.Sum(nil)
}

func NewHMACSha256(secret string) *HMACSha256 {
	return &HMACSha256{hmac.New(sha256.New, []byte(secret))}
}

// Entry stores information about a hashed IP.
type Entry struct {
	LastVisit time.Time // Last visit from user
	// Count     int       // Visit count
}

func (e *Entry) Encode() []byte {
	unix := e.LastVisit.Unix()
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, uint64(unix))
	return out
}

func DecodeEntry(b []byte) (e *Entry) {
	e = &Entry{time.Unix(int64(binary.LittleEndian.Uint64(b)), 0)}
	return
}

type BoltDB struct {
	db   *bolt.DB
	name []byte
	lock *sync.Mutex
}

func NewBoltDB(fname, name string) (*BoltDB, error) {
	b := &BoltDB{}
	b.lock = &sync.Mutex{}
	b.name = []byte(name)
	var err error
	b.db, err = bolt.Open(fname, 0600, nil)
	if err != nil {
		return nil, err
	}
	err = b.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(b.name)
		return err
	})
	if err != nil {
		return nil, err
	}
	return b, err
}

func (b *BoltDB) Close() {
	b.db.Close()
}

func (b *BoltDB) LogVisit(hash []byte, t time.Time) error {
	entry := &Entry{t}
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.name)
		return bucket.Put(hash, entry.Encode())
	})
}

func (b *BoltDB) GetEntry(hash []byte) *Entry {
	var e *Entry
	b.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.name)
		e = DecodeEntry(bucket.Get(hash))
		return nil
	})
	return e
}

func (b *BoltDB) CountActive(ActiveThreshold *Threshold) (count int) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.name)
		var e *Entry
		bucket.ForEach(func(k, v []byte) error {
			e = DecodeEntry(v)
			if ActiveThreshold.Valid(e.LastVisit) {
				count++
			} else {
				bucket.Delete(k)
			}
			return nil
		})
		return nil
	})
	return
}

type DB interface {
	Close()
	LogVisit([]byte, time.Time) error
	GetEntry([]byte) *Entry
	CountActive(*Threshold) int
}

type Server struct {
	db              DB
	hash            Hash
	config          *ini.File
	ActiveThreshold *Threshold
}

func (s *Server) HandleVisit(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hash := s.hash.Hash(ip)
	s.db.LogVisit(hash, time.Now())
	// log.Printf("Logged ip %s", ip)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleCount(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%d", s.db.CountActive(s.ActiveThreshold))
}

func (s *Server) Serve() {
	http.HandleFunc("/add", s.HandleVisit)
	http.HandleFunc("/count", s.HandleCount)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", s.config.Section("").Key("port").MustInt(8000)), nil))
}

func NewServer(configpath, fname, name string) (*Server, error) {
	s := &Server{}
	var err error
	s.db, err = NewBoltDB(fname, name)
	if err != nil {
		return nil, err
	}
	s.config, err = ini.Load(configpath)
	if err != nil {
		return nil, err
	}
	s.ActiveThreshold = NewThreshold(s.config.Section("").Key("days").MustInt(0), s.config.Section("").Key("hours").MustInt(2), s.config.Section("").Key("minutes").MustInt(0))
	s.hash = NewHMACSha256(s.config.Section("").Key("secret").String())
	return s, err
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("usage: %s <path to config.ini> <path to ip.db>", os.Args[0])
	}
	s, err := NewServer(os.Args[len(os.Args)-2], os.Args[len(os.Args)-1], NAME)
	if err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
	defer s.db.Close()
	s.Serve()
}
