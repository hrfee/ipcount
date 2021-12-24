package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
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
	CountryISOCode string // GeoIP2 Country ID.
}

func (e *Entry) Encode() []byte {
	unix := e.LastVisit.Unix()
	out := make([]byte, 16)
	country := []byte(e.CountryISOCode)
	binary.LittleEndian.PutUint64(out, uint64(unix))
	for i := 8; i < 8+len(country); i++ {
		out[i] = country[i-8]
	}
	return out
}

func DecodeEntry(b []byte) (e *Entry) {
	e = &Entry{}
	e.CountryISOCode = ""
	for i := 8; i < 16; i++ {
		if b[i] != 0 {
			e.CountryISOCode += string(b[i])
			b[i] = 0
		}
	}
	e.LastVisit = time.Unix(int64(binary.LittleEndian.Uint64(b)), 0)
	return
}

type BoltDB struct {
	db    *bolt.DB
	name  []byte
	lock  *sync.Mutex
	GeoIP bool
	GeoDB *geoip2.Reader
}

func NewBoltDB(fname, name, geoip string) (*BoltDB, error) {
	b := &BoltDB{GeoIP: geoip != ""}
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
	if b.GeoIP {
		b.GeoDB, err = geoip2.Open(geoip)
		if err != nil {
			return nil, err
		}
	}
	return b, err
}

func (b *BoltDB) Close() {
	b.db.Close()
	if b.GeoIP {
		b.GeoDB.Close()
	}
}

func (b *BoltDB) LogVisit(hash []byte, t time.Time, ip ...string) error {
	entry := &Entry{LastVisit: t}
	if len(ip) == 1 {
		record, err := b.GeoDB.Country(net.ParseIP(ip[0]))
		if err == nil {
			entry.CountryISOCode = record.Country.IsoCode
			// } else {
			// 	fmt.Println(err)
		}
	}
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

func (b *BoltDB) CountByCountry(ActiveThreshold *Threshold) (counts map[string]int) {
	counts = map[string]int{"Total": 0}
	b.lock.Lock()
	defer b.lock.Unlock()
	b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(b.name)
		var e *Entry
		bucket.ForEach(func(k, v []byte) error {
			e = DecodeEntry(v)
			if ActiveThreshold.Valid(e.LastVisit) {
				counts["Total"]++
				if e.CountryISOCode == "" {
					if _, ok := counts["Unknown"]; !ok {
						counts["Unknown"] = 0
					}
					counts["Unknown"]++
				} else {
					if _, ok := counts[e.CountryISOCode]; !ok {
						counts[e.CountryISOCode] = 0
					}
					counts[e.CountryISOCode]++
				}
			} else {
				bucket.Delete(k)
			}
			return nil
		})
		return nil
	})
	return counts
}

type DB interface {
	Close()
	LogVisit([]byte, time.Time, ...string) error
	GetEntry([]byte) *Entry
	CountActive(*Threshold) int
	CountByCountry(*Threshold) map[string]int
}

type Server struct {
	db              DB
	hash            Hash
	config          *ini.File
	ActiveThreshold *Threshold
	GeoIP           bool
}

func (s *Server) HandleVisit(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hash := s.hash.Hash(ip)
	if s.GeoIP {
		s.db.LogVisit(hash, time.Now(), ip)
	} else {
		s.db.LogVisit(hash, time.Now())
	}
	// log.Printf("Logged ip %s", ip)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleCount(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "%d", s.db.CountActive(s.ActiveThreshold))
}

func (s *Server) HandleCountries(w http.ResponseWriter, r *http.Request) {
	b, err := json.MarshalIndent(s.db.CountByCountry(s.ActiveThreshold), "", "	")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}

func (s *Server) Serve() {
	http.HandleFunc("/add", s.HandleVisit)
	http.HandleFunc("/count", s.HandleCount)
	http.HandleFunc("/countries", s.HandleCountries)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", s.config.Section("").Key("port").MustInt(8000)), nil))
}

func NewServer(configpath, fname, name string) (*Server, error) {
	s := &Server{}
	var err error
	s.config, err = ini.Load(configpath)
	if err != nil {
		return nil, err
	}
	geoip := s.config.Section("").Key("geoip2_db").String()
	if geoip != "" {
		s.GeoIP = true
	}
	s.db, err = NewBoltDB(fname, name, geoip)
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
