package morgoth

import (
	"bytes"
	"encoding/gob"
	"github.com/nathanielc/morgoth/Godeps/_workspace/src/github.com/boltdb/bolt"
	"github.com/nathanielc/morgoth/Godeps/_workspace/src/github.com/golang/glog"
	"runtime"
)

const queryBufferSize = 100

var workerCount = runtime.NumCPU()

type Manager struct {
	scheduledDataQueries []*scheduledDataQuery
	mapper               *Mapper
	engine               Engine
	queryQueue           chan dataQuery
	db                   *bolt.DB
}

func NewManager(mapper *Mapper, engine Engine, scheduledDataQueries []*scheduledDataQuery) *Manager {
	return &Manager{
		scheduledDataQueries: scheduledDataQueries,
		mapper:               mapper,
		engine:               engine,
		queryQueue:           make(chan dataQuery, queryBufferSize),
	}
}

func (self *Manager) Start() (err error) {
	for _, sq := range self.scheduledDataQueries {
		sq.Start(self.queryQueue)
	}

	self.db, err = bolt.Open("morgoth.db", 0600, nil)
	if err != nil {
		return
	}

	self.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("morgoth"))
		if err != nil {
			return err
		}
		return nil
	})
	self.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("morgoth"))
		mappingBytes := bytes.NewBuffer(b.Get([]byte("mappings")))
		dec := gob.NewDecoder(mappingBytes)
		mappings := make([]*DetectorMapper, 0)
		err := dec.Decode(&mappings)
		if err != nil {
			return err
		}
		for _, m := range mappings {
			self.mapper.addDetectorMapper(m)
		}
		return nil
	})

	glog.V(1).Infof("Starting %d processQueries routines", workerCount)
	for i := 0; i < workerCount; i++ {
		go self.processQueries()
	}
	return
}

func (self *Manager) Stop() {
	close(self.queryQueue)

	self.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("morgoth"))
		var mappings bytes.Buffer
		enc := gob.NewEncoder(&mappings)
		enc.Encode(self.mapper.GetDetectorMappings())
		err := b.Put([]byte("mappings"), mappings.Bytes())
		return err
	})
	self.db.Close()
}

func (self *Manager) processQueries() {
	for query := range self.queryQueue {
		glog.V(2).Info("Executing query:", query)
		windows, err := self.engine.GetWindows(query.query)
		if err != nil {
			glog.Errorf("Failed to execute query: '%s' %s", query, err)
			continue
		}
		// Tag windows with query tags
		glog.V(3).Info("Adding query tags: ", query.tags)
		for _, w := range windows {
			for t, v := range query.tags {
				w.Tags[t] = v
			}
		}

		self.ProcessWindows(windows)
	}
}

func (self *Manager) ProcessWindows(windows []*Window) {

	var detector *Detector
	for _, w := range windows {
		detector = self.mapper.Map(w)
		if detector == nil {
			glog.Warningf("No mapping found for window %s %s", w.Name, w.Tags)
			continue
		}

		if detector.IsAnomalous(w) {
			self.RecordAnomalous(w)
		}
	}
}

func (self *Manager) RecordAnomalous(w *Window) {
	glog.Infof("Found anomalous window: %s %s %s", w.Name, w.Tags, w.Start)
	err := self.engine.RecordAnomalous(w.Copy())
	if err != nil {
		glog.Errorf("Error recording anomaly: %s", err)
	}
}

type scheduledDataQuery struct {
	sq    *ScheduledQuery
	tags  map[string]string
	queue chan dataQuery
}

func (self *scheduledDataQuery) Start(queue chan dataQuery) {
	self.queue = queue
	self.sq.Callback = self.callback
	self.sq.Start()
}

func (self *scheduledDataQuery) callback(query Query) {
	dq := dataQuery{
		query: query,
		tags:  self.tags,
	}
	self.queue <- dq
}

type dataQuery struct {
	query Query
	tags  map[string]string
}
