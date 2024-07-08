package server

import (
	"context"

	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
)

// The functions below are Server's Raw API. (implements TinyKvServer).
// Some helper methods can be found in sever.go in the current directory

// RawGet return the corresponding Get response based on RawGetRequest's CF and Key fields
func (server *Server) RawGet(_ context.Context, req *kvrpcpb.RawGetRequest) (*kvrpcpb.RawGetResponse, error) {
	// Your Code Here (1).
	reader, _ := server.storage.Reader(req.Context)
	value, _ := reader.GetCF(req.Cf, req.Key)
	notFound := value == nil
	return &kvrpcpb.RawGetResponse{
		Value:    value,
		NotFound: notFound,
	}, nil
}

// RawPut puts the target data into storage and returns the corresponding response
func (server *Server) RawPut(_ context.Context, req *kvrpcpb.RawPutRequest) (*kvrpcpb.RawPutResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using Storage.Modify to store data to be modified
	_ = server.storage.Write(req.Context, []storage.Modify{
		{
			Data: storage.Put{
				Cf:    req.Cf,
				Key:   req.Key,
				Value: req.Value,
			},
		},
	})
	// 暂时不知道要往 RawPutResponce 里面写什么，弄个空的
	return &kvrpcpb.RawPutResponse{}, nil
}

// RawDelete delete the target data from storage and returns the corresponding response
func (server *Server) RawDelete(_ context.Context, req *kvrpcpb.RawDeleteRequest) (*kvrpcpb.RawDeleteResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using Storage.Modify to store data to be deleted
	_ = server.storage.Write(req.Context, []storage.Modify{
		{
			Data: storage.Delete{
				Cf:  req.Cf,
				Key: req.Key,
			},
		},
	})
	return &kvrpcpb.RawDeleteResponse{}, nil
}

// RawScan scan the data starting from the start key up to limit. and return the corresponding result
func (server *Server) RawScan(_ context.Context, req *kvrpcpb.RawScanRequest) (*kvrpcpb.RawScanResponse, error) {
	// Your Code Here (1).
	// Hint: Consider using reader.IterCF
	reader, _ := server.storage.Reader(req.Context)
	it := reader.IterCF(req.Cf)
	var count uint32 = 0
	var kvs []*kvrpcpb.KvPair
	for it.Seek(req.StartKey); count < req.Limit && it.Valid(); count++ {
		newPair := &kvrpcpb.KvPair{
			Key: it.Item().Key(),
		}
		newPair.Value, _ = it.Item().Value()
		kvs = append(kvs, newPair)
		it.Next()
	}
	return &kvrpcpb.RawScanResponse{
		Kvs: kvs,
	}, nil
}
