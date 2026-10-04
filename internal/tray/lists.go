package tray

import (
	"context"
	"sync"

	"fyne.io/systray"
)

// row is one checkbox in a dynamic submenu.
type row struct {
	ID       string
	Title    string
	Tooltip  string
	Checked  bool
	Disabled bool
}

// list keeps a submenu's checkboxes in sync with a changing set of rows.
// New rows are appended; rows that disappear are removed.
type list struct {
	ctx    context.Context
	parent *systray.MenuItem
	toggle func(id string, on bool)

	mu    sync.Mutex
	items map[string]*listItem
}

type listItem struct {
	item *systray.MenuItem
	stop chan struct{}
}

func newList(ctx context.Context, parent *systray.MenuItem, toggle func(id string, on bool)) *list {
	return &list{ctx: ctx, parent: parent, toggle: toggle, items: map[string]*listItem{}}
}

func (l *list) sync(rows []row) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := map[string]bool{}
	for _, r := range rows {
		keep[r.ID] = true
		li, ok := l.items[r.ID]
		if !ok {
			li = &listItem{item: l.parent.AddSubMenuItemCheckbox(r.Title, r.Tooltip, r.Checked), stop: make(chan struct{})}
			l.items[r.ID] = li
			go l.watch(r.ID, li)
		}
		li.item.SetTitle(r.Title)
		li.item.SetTooltip(r.Tooltip)
		setChecked(li.item, r.Checked)
		if r.Disabled {
			li.item.Disable()
		} else {
			li.item.Enable()
		}
	}
	for id, li := range l.items {
		if !keep[id] {
			li.item.Remove()
			close(li.stop)
			delete(l.items, id)
		}
	}
}

func (l *list) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

func (l *list) watch(id string, li *listItem) {
	for {
		select {
		case <-li.item.ClickedCh:
			on := !li.item.Checked()
			setChecked(li.item, on)
			l.toggle(id, on)
		case <-li.stop:
			return
		case <-l.ctx.Done():
			return
		}
	}
}

func setChecked(item *systray.MenuItem, on bool) {
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
}
