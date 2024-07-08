package standalone_storage

import (
	"sort"

	"github.com/pingcap-incubator/tinykv/kv/config"
	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
)

// StandAloneStorage is an implementation of `Storage` for a single-node TinyKV instance. It does not
// communicate with other nodes and all data is stored locally.
type StandAloneStorage struct {
	// Your Data Here (1).
	Conf config.Config
	Data map[string]map[byte][]byte
}

func NewStandAloneStorage(conf *config.Config) *StandAloneStorage {
	// Your Code Here (1).
	return &StandAloneStorage{
		Conf: *conf,
	}
}

func (s *StandAloneStorage) Start() error {
	// Your Code Here (1).
	return nil
}

func (s *StandAloneStorage) Stop() error {
	// Your Code Here (1).
	return nil
}

func (s *StandAloneStorage) Reader(ctx *kvrpcpb.Context) (storage.StorageReader, error) {
	// Your Code Here (1).
	return s, nil
}

func (s *StandAloneStorage) Write(ctx *kvrpcpb.Context, batch []storage.Modify) error {
	// Your Code Here (1).
	// 实现思路：StandAloneStorage 的 Data 映射 string -> map[byte][]byte
	if s.Data == nil {
		s.Data = make(map[string]map[byte][]byte)
	}
	for _, batchItem := range batch {
		switch batchItem.Data.(type) {
		case storage.Put:
			keyValMap, cfExists := s.Data[batchItem.Cf()]
			if !cfExists {
				// 不存在对应 ColumnFamily 时
				keyValMap = make(map[byte][]byte)
				s.Data[batchItem.Cf()] = keyValMap
			}
			for _, key := range batchItem.Key() {
				keyValMap[key] = append(keyValMap[key], batchItem.Value()...)
			}
		case storage.Delete:
			keyValMap, cfExists := s.Data[batchItem.Cf()]
			if cfExists {
				// 只有存在对应的 ColumnFamily 才可以删除
				for _, key := range batchItem.Key() {
					delete(keyValMap, key)
				}
			}
		}
	}
	return nil
}

/*自己增加三个函数，使得 StandAloneStorage 实现接口 storage.StorageReader*/

func (s *StandAloneStorage) GetCF(cf string, key []byte) ([]byte, error) {
	keyValMap, cfExists := s.Data[cf]
	var value []byte
	if cfExists {
		for _, kkey := range key {
			if vvalue, ok := keyValMap[kkey]; ok {
				value = append(value, vvalue...)
			}
			// 这里直接忽略了部分 key 不存在,但是部分 key 存在的情况
			// 所有 key 均不存在，返回 nil
		}
		return value, nil
	}
	return nil, nil
}

func (s *StandAloneStorage) IterCF(cf string) engine_util.DBIterator {
	var keys []byte
	mmap := make(map[byte][]byte)
	for key, value := range s.Data[cf] {
		mmap[key] = value
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return &StandAloneIterator{
		Data:  mmap,
		Key:   keys,
		index: 0,
	}
}

func (s *StandAloneStorage) Close() {}

/*构造结构体 StandAloneIterator 实现接口 DBIterator,是当前数据库的快照*/

type StandAloneIterator struct {
	index int
	Key   []byte
	Data  map[byte][]byte
}

func (s *StandAloneIterator) Item() engine_util.DBItem {
	if !s.Valid() {
		return nil
	}
	vvalue := make([]byte, len(s.Data[s.Key[s.index]]))
	copy(vvalue, s.Data[s.Key[s.index]])
	return &StandAloneItem{
		Kkey:   []byte{s.Key[s.index]},
		Vvalue: vvalue,
	}
}

func (s *StandAloneIterator) Valid() bool { return s.index < len(s.Key) }
func (s *StandAloneIterator) Next()       { s.index++ }
func (s *StandAloneIterator) Seek(val []byte) {
	s.index = sort.Search(len(s.Key), func(i int) bool { return s.Key[i] >= val[0] })
	// 如果不存在，index 指向最末尾
}
func (s *StandAloneIterator) Close() {}

/*构造结构体 StandAloneItem 实现接口 DBItem*/

type StandAloneItem struct {
	Kkey   []byte
	Vvalue []byte
}

func (s *StandAloneItem) Key() []byte { return s.Kkey }
func (s *StandAloneItem) KeyCopy(dst []byte) []byte {
	if len(dst) < len(s.Kkey) {
		dst = make([]byte, len(s.Kkey))
	}
	copy(dst, s.Kkey)
	return dst
}
func (s *StandAloneItem) Value() ([]byte, error) { return s.Vvalue, nil }
func (s *StandAloneItem) ValueSize() int         { return len(s.Vvalue) }
func (s *StandAloneItem) ValueCopy(dst []byte) ([]byte, error) {
	if len(dst) < len(s.Vvalue) {
		dst = make([]byte, len(s.Vvalue))
	}
	copy(dst, s.Vvalue)
	return dst, nil
}
