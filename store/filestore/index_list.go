package filestore

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/smancke/guble/store"
)

// IndexList a sorted list of fetch entries
type IndexList struct {
	items []*Index

	sync.RWMutex
}

func newList(size int) *IndexList {
	return &IndexList{items: make([]*Index, 0, size)}
}

func (l *IndexList) Len() int {
	l.RLock()
	defer l.RUnlock()

	return len(l.items)
}

func (l *IndexList) InsertList(other *IndexList) {
	l.Insert(other.Items()...)
}

//Insert  adds in the sorted list a new element
func (l *IndexList) Insert(items ...*Index) {
	for _, elem := range items {
		l.insertElem(elem)
	}
}

func (l *IndexList) insertElem(elem *Index) {
	l.Lock()
	defer l.Unlock()

	// first element on list just append at the end
	if len(l.items) == 0 {
		l.items = append(l.items, elem)
		return
	}

	// if the first element in list have a bigger id...insert new element on the start of list
	if l.items[0].id >= elem.id {
		l.items = append([]*Index{elem}, l.items...)
		return
	}

	if l.items[len(l.items)-1].id <= elem.id {
		l.items = append(l.items, elem)
		return
	}

	//found the correct position to make an insertion sort
	for i := 1; i <= len(l.items)-1; i++ {
		if l.items[i].id > elem.id {
			l.items = append(l.items[:i], append([]*Index{elem}, l.items[i:]...)...)
			return
		}
	}
}

// Clear empties the current list
func (l *IndexList) Clear() {
	l.items = make([]*Index, 0)
}

// GetIndexEntryFromID performs a binarySearch retrieving the
// true, the position and list and the actual entry if found
// false , -1 ,nil if position is not found
// search performs a binary search returning:
// - `true` in case the item was found
// - `position` position of the item
// - `bestIndex` the closest index to the searched item if not found.
// - `index` the index if found
func (l *IndexList) search(searchID uint64) (bool, int, int, *Index) {
	l.RLock()
	defer l.RUnlock()

	if len(l.items) == 0 {
		return false, -1, -1, nil
	}

	h := len(l.items) - 1
	f := 0
	bestIndex := f

	for f <= h {
		mid := (h + f) / 2
		if l.items[mid].id == searchID {
			return true, mid, bestIndex, l.items[mid]
		} else if l.items[mid].id < searchID {
			f = mid + 1
		} else {
			h = mid - 1
		}

		if abs(l.items[mid].id, searchID) <= abs(l.items[bestIndex].id, searchID) {
			bestIndex = mid
		}
	}

	return false, -1, bestIndex, nil
}

//Back retrieves the element with the biggest id or nil if list is empty
func (l *IndexList) Back() *Index {
	l.RLock()
	defer l.RUnlock()

	if len(l.items) == 0 {
		return nil
	}

	return l.items[len(l.items)-1]
}

//Front retrieves the element with the smallest id or nil if list is empty
func (l *IndexList) Front() *Index {
	l.RLock()
	defer l.RUnlock()

	if len(l.items) == 0 {
		return nil
	}

	return l.items[0]
}

func (l *IndexList) Items() []*Index {
	l.RLock()
	defer l.RUnlock()

	return l.items
}

//Front retrieves the element at the given index or nil if position is incorrect or list is empty
func (l *IndexList) Get(pos int) *Index {
	l.RLock()
	defer l.RUnlock()

	if len(l.items) == 0 || pos < 0 || pos >= len(l.items) {
		logger.WithFields(log.Fields{
			"len": len(l.items),
			"pos": pos,
		}).Info("Empty list or invalid index")
		return nil
	}

	return l.items[pos]
}

func (l *IndexList) Do(predicate func(elem *Index, i int) error) error {
	l.RLock()
	defer l.RUnlock()

	for i, elem := range l.items {
		if err := predicate(elem, i); err != nil {
			return err
		}
	}

	return nil
}

func (l *IndexList) String() string {
	l.RLock()
	defer l.RUnlock()

	s := ""
	for i, elem := range l.items {
		s += fmt.Sprintf("[%d:%d %d] ", i, elem.id, elem.fileID)
	}
	return s
}

// Contains returns true if given ID is between first and last item in the list
func (l *IndexList) Contains(id uint64) bool {
	l.RLock()
	defer l.RUnlock()

	if len(l.items) == 0 {
		return false
	}

	return l.items[0].id <= id && id <= l.items[len(l.items)-1].id
}

// Extract will return a new list containing items requested by the FetchRequest
func (l *IndexList) Extract(req *store.FetchRequest) *IndexList {
	potentialEntries := newList(0)
	found, pos, lastPos, _ := l.search(req.StartID)
	currentPos := lastPos
	if found == true {
		currentPos = pos
	}

	for potentialEntries.Len() < req.Count && currentPos >= 0 && currentPos < l.Len() {
		elem := l.Get(currentPos)
		logger.WithFields(log.Fields{
			"elem":       *elem,
			"currentPos": currentPos,
			"req":        *req,
		}).Debug("Elem in retrieve")

		if elem == nil {
			logger.WithFields(log.Fields{
				"pos":     currentPos,
				"l.Len":   l.Len(),
				"len":     potentialEntries.Len(),
				"startID": req.StartID,
				"count":   req.Count,
			}).Error("Error in retrieving from list.Got nil entry")
			break
		}

		potentialEntries.Insert(elem)
		currentPos += req.Direction
	}
	return potentialEntries
}

func abs(m1, m2 uint64) uint64 {
	if m1 > m2 {
		return m1 - m2
	}

	return m2 - m1
}
