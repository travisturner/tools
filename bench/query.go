package bench

import (
	"fmt"
	"math/rand"
	"time"

	"context"

	"os"

	"github.com/pilosa/pilosa/pql"
)

type Query struct {
	HasClient
	Name       string `json:"name"`
	Query      string `json:"query"`
	Index      string `json:"index"`
	Iterations int    `json:"iterations"`
}

// Init sets up the pilosa client and modifies the configured values based on
// the agent num.
func (b *Query) Init(hosts []string, agentNum int) error {
	b.Name = "query"
	return b.HasClient.Init(hosts, agentNum)
}

// Run runs the Query benchmark
func (b *Query) Run(ctx context.Context) *Result {
	results := NewResult()
	if b.client == nil {
		results.err = fmt.Errorf("No client set for Query benchmark")
		return results
	}
	for n := 0; n < b.Iterations; n++ {
		start := time.Now()
		res, err := b.ExecuteQuery(ctx, b.Index, b.Query)
		fmt.Fprintf(os.Stderr, "results obj: %v, start time: %v, res: %v", results, start, res)
		results.Add(time.Since(start), res)
		if err != nil {
			results.err = fmt.Errorf("problem with query #%d: %v", n, err)
			return results
		}
	}
	return results
}

// BasicQuery runs a query against pilosa multiple times with increasing row
// ids.
type BasicQuery struct {
	HasClient
	Name       string `json:"name"`
	BaseRowID  int64  `json:"base-row-id"`
	Iterations int    `json:"iterations"`
	NumArgs    int    `json:"num-args"`
	Query      string `json:"query"`
	Index      string `json:"index"`
	Frame      string `json:"frame"`
}

// Init sets up the pilosa client and modifies the configured values based on
// the agent num.
func (b *BasicQuery) Init(hosts []string, agentNum int) error {
	b.Name = "basic-query"
	b.BaseRowID = b.BaseRowID + int64(agentNum*b.Iterations)
	return b.HasClient.Init(hosts, agentNum)
}

// Run runs the BasicQuery benchmark
func (b *BasicQuery) Run(ctx context.Context) *Result {
	results := NewResult()
	if b.client == nil {
		results.err = fmt.Errorf("No client set for BasicQuery")
		return results
	}

	bms := make([]*pql.Call, b.NumArgs)
	for i, _ := range bms {
		bms[i] = &pql.Call{
			Name: "Bitmap",
			Args: map[string]interface{}{"frame": b.Frame},
		}
	}
	query := pql.Call{
		Name: b.Query,
	}
	var start time.Time
	for n := 0; n < b.Iterations; n++ {
		for i, _ := range bms {
			bms[i].Args["rowID"] = b.BaseRowID + int64(n)
		}
		query.Children = bms
		start = time.Now()
		_, err := b.ExecuteQuery(ctx, b.Index, query.String())
		results.Add(time.Since(start), nil)
		if err != nil {
			results.err = err
			return results
		}
	}
	return results
}

// NewQueryGenerator initializes a new QueryGenerator
func NewQueryGenerator(seed int64) *QueryGenerator {
	return &QueryGenerator{
		IDToFrameFn: func(id uint64) string { return "fbench" },
		R:           rand.New(rand.NewSource(seed)),
		Frames:      []string{"fbench"},
	}
}

// QueryGenerator holds the configuration and state for randomly generating
// queries.
type QueryGenerator struct {
	IDToFrameFn func(id uint64) string
	R           *rand.Rand
	Frames      []string
}

// Random returns a randomly generated query.
func (q *QueryGenerator) Random(maxN, depth, maxargs int, idmin, idmax uint64) *pql.Call {
	// TODO: handle depth==1 or 0
	val := q.R.Intn(5)
	switch val {
	case 0:
		return q.RandomTopN(maxN, depth, maxargs, idmin, idmax)
	default:
		return q.RandomBitmapCall(depth, maxargs, idmin, idmax)
	}
}

// RandomTopN returns a randomly generated TopN query.
func (q *QueryGenerator) RandomTopN(maxN, depth, maxargs int, idmin, idmax uint64) *pql.Call {
	frameIdx := q.R.Intn(len(q.Frames))
	return &pql.Call{
		Name: "TopN",
		Args: map[string]interface{}{
			"frame": q.Frames[frameIdx],
			"n":     uint64(q.R.Intn(maxN-1) + 1),
		},
		Children: []*pql.Call{q.RandomBitmapCall(depth, maxargs, idmin, idmax)},
	}
}

// RandomBitmapCall returns a randomly generate query which returns a bitmap.
func (q *QueryGenerator) RandomBitmapCall(depth, maxargs int, idmin, idmax uint64) *pql.Call {
	if depth <= 1 {
		rowID := q.R.Int63n(int64(idmax)-int64(idmin)) + int64(idmin)
		return Bitmap(uint64(rowID), q.IDToFrameFn(uint64(rowID)))
	}
	call := q.R.Intn(4)
	if call == 0 {
		return q.RandomBitmapCall(1, 0, idmin, idmax)
	}

	var numargs int
	if maxargs <= 2 {
		numargs = 2
	} else {
		numargs = q.R.Intn(maxargs-2) + 2
	}
	calls := make([]*pql.Call, numargs)
	for i := 0; i < numargs; i++ {
		calls[i] = q.RandomBitmapCall(depth-1, maxargs, idmin, idmax)
	}

	switch call {
	case 1:
		return Difference(calls...)
	case 2:
		return Intersect(calls...)
	case 3:
		return Union(calls...)
	}
	return nil
}

///////////////////////////////////////////////////
// Helpers TODO: move elsewhere
///////////////////////////////////////////////////

func ClearBit(id uint64, frame string, columnID uint64) *pql.Call {
	return &pql.Call{
		Name: "ClearBit",
		Args: map[string]interface{}{
			"id":       id,
			"frame":    frame,
			"columnID": columnID,
		},
	}
}

func Count(child *pql.Call) *pql.Call {
	return &pql.Call{
		Name:     "Count",
		Children: []*pql.Call{child},
	}
}

func Column(id uint64) *pql.Call {
	return &pql.Call{
		Name: "Column",
		Args: map[string]interface{}{"id": id},
	}
}

func SetBit(id uint64, frame string, columnID uint64) *pql.Call {
	return &pql.Call{
		Name: "SetBit",
		Args: map[string]interface{}{
			"id":       id,
			"frame":    frame,
			"columnID": columnID,
		},
	}
}

func SetRowAttrs(id uint64, frame string, attrs map[string]interface{}) *pql.Call {
	args := copyArgs(attrs)
	args["id"] = id
	args["columnID"] = frame

	return &pql.Call{
		Name: "SetRowAttrs",
		Args: args,
	}
}

func SetColumnAttrs(id uint64, attrs map[string]interface{}) *pql.Call {
	args := copyArgs(attrs)
	args["id"] = id

	return &pql.Call{
		Name: "SetColumnAttrs",
		Args: args,
	}
}

func TopN(frame string, n int, src *pql.Call, bmids []uint64, field string, filters []interface{}) *pql.Call {
	return &pql.Call{
		Name:     "TopN",
		Children: []*pql.Call{src},
		Args: map[string]interface{}{
			"frame":   frame,
			"n":       n,
			"ids":     bmids,
			"field":   field,
			"filters": filters,
		},
	}
}

func Difference(bms ...*pql.Call) *pql.Call {
	// TODO does this need to be limited to two inputs?
	return &pql.Call{
		Name:     "Difference",
		Children: bms,
	}
}

func Intersect(bms ...*pql.Call) *pql.Call {
	return &pql.Call{
		Name:     "Intersect",
		Children: bms,
	}
}

func Union(bms ...*pql.Call) *pql.Call {
	return &pql.Call{
		Name:     "Union",
		Children: bms,
	}
}

func Bitmap(id uint64, frame string) *pql.Call {
	return &pql.Call{
		Name: "Bitmap",
		Args: map[string]interface{}{
			"rowID": id,
			"frame": frame,
		},
	}
}

// copyArgs returns a shallow copy of m.
func copyArgs(m map[string]interface{}) map[string]interface{} {
	other := make(map[string]interface{}, len(m))
	for k, v := range m {
		other[k] = v
	}
	return other
}
