package dialog

import (
	"strings"

	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/sahilm/fuzzy"
)

// ModelsList is a list specifically for model items and groups.
type ModelsList struct {
	*list.List
	groups []ModelGroup
	query  string
	t      *styles.Styles

	// emptyMessage is shown when there are no items and no active filter.
	emptyMessage string
	// filterEmptyMessage is shown when a filter is active but matches nothing.
	filterEmptyMessage string
}

// NewModelsList creates a new list suitable for model items and groups.
func NewModelsList(sty *styles.Styles, groups ...ModelGroup) *ModelsList {
	f := &ModelsList{
		List:   list.NewList(),
		groups: groups,
		t:      sty,
	}
	f.RegisterRenderCallback(list.FocusedRenderCallback(f.List))
	return f
}

// Len returns the number of model items across all groups.
func (f *ModelsList) Len() int {
	n := 0
	for _, g := range f.groups {
		n += len(g.Items)
	}
	return n
}

// SetGroups sets the model groups and updates the list items.
func (f *ModelsList) SetGroups(groups ...ModelGroup) {
	f.groups = groups
	items := []list.Item{}
	for _, g := range f.groups {
		items = append(items, &g)
		for _, item := range g.Items {
			items = append(items, item)
		}
		// Add a space separator after each provider section
		items = append(items, list.NewSpacerItem(1))
	}
	f.SetItems(items...)
}

// SetFilter sets the filter query and updates the list items.
func (f *ModelsList) SetFilter(q string) {
	f.query = q
	f.SetItems(f.VisibleItems()...)
}

// SetSelected sets the selected item index. It overrides the base method to
// skip non-model items.
func (f *ModelsList) SetSelected(index int) {
	if index < 0 || index >= f.Len() {
		f.List.SetSelected(index)
		return
	}

	f.List.SetSelected(index)
	for {
		selectedItem := f.SelectedItem()
		if _, ok := selectedItem.(*ModelItem); ok {
			return
		}
		f.List.SetSelected(index + 1)
		index++
		if index >= f.Len() {
			return
		}
	}
}

// SetSelectedItem sets the selected item in the list by item ID.
func (f *ModelsList) SetSelectedItem(itemID string) {
	if itemID == "" {
		return
	}

	// Walk the selectable model items using the same helpers that
	// keyboard navigation uses, so we stay in sync with the flat
	// list layout.
	for ok := f.SelectFirst(); ok; ok = f.SelectNext() {
		if mi, is := f.SelectedItem().(*ModelItem); is && mi.ID() == itemID {
			return
		}
	}
}

// SelectNext selects the next model item, skipping any non-focusable items
// like group headers and spacers.
func (f *ModelsList) SelectNext() (v bool) {
	v = f.List.SelectNext()
	for v {
		selectedItem := f.SelectedItem()
		if _, ok := selectedItem.(*ModelItem); ok {
			return v
		}
		v = f.List.SelectNext()
	}
	return v
}

// SelectPrev selects the previous model item, skipping any non-focusable items
// like group headers and spacers.
func (f *ModelsList) SelectPrev() (v bool) {
	v = f.List.SelectPrev()
	for v {
		selectedItem := f.SelectedItem()
		if _, ok := selectedItem.(*ModelItem); ok {
			return v
		}
		v = f.List.SelectPrev()
	}
	return v
}

// SelectFirst selects the first model item in the list.
func (f *ModelsList) SelectFirst() (v bool) {
	v = f.List.SelectFirst()
	for v {
		selectedItem := f.SelectedItem()
		_, ok := selectedItem.(*ModelItem)
		if ok {
			return v
		}
		v = f.List.SelectNext()
	}
	return v
}

// SelectLast selects the last model item in the list.
func (f *ModelsList) SelectLast() (v bool) {
	v = f.List.SelectLast()
	for v {
		selectedItem := f.SelectedItem()
		if _, ok := selectedItem.(*ModelItem); ok {
			return v
		}
		v = f.List.SelectPrev()
	}
	return v
}

// IsSelectedFirst checks if the selected item is the first model item.
func (f *ModelsList) IsSelectedFirst() bool {
	originalIndex := f.Selected()
	f.SelectFirst()
	isFirst := f.Selected() == originalIndex
	f.List.SetSelected(originalIndex)
	return isFirst
}

// IsSelectedLast checks if the selected item is the last model item.
func (f *ModelsList) IsSelectedLast() bool {
	originalIndex := f.Selected()
	f.SelectLast()
	isLast := f.Selected() == originalIndex
	f.List.SetSelected(originalIndex)
	return isLast
}

// VisibleItems returns the visible items after filtering.
func (f *ModelsList) VisibleItems() []list.Item {
	query := strings.ToLower(strings.ReplaceAll(f.query, " ", ""))

	if query == "" {
		// No filter, return all items with group headers
		items := []list.Item{}
		for _, g := range f.groups {
			items = append(items, &g)
			for _, item := range g.Items {
				item.SetMatch(fuzzy.Match{})
				items = append(items, item)
			}
			// Add a space separator after each provider section
			items = append(items, list.NewSpacerItem(1))
		}
		return items
	}

	// Build the match text once across all items (O(N) rather than O(N*G)).
	// Each entry is prefixed with its group's lowercased title, exactly as the
	// previous per-group code matched against.
	names := make([]string, 0, f.Len())
	for _, g := range f.groups {
		prefix := strings.ToLower(g.Title) + " "
		for _, item := range g.Items {
			names = append(names, prefix+item.Filter())
		}
	}

	// Single fuzzy pass, then bucket matches back to their group by index.
	matches := fuzzy.Find(query, names)
	matched := make([]*fuzzy.Match, len(names))
	for i := range matches {
		matched[matches[i].Index] = &matches[i]
	}

	items := []list.Item{}

	// Reconstruct groups with matched items, preserving original group and
	// within-group item order.
	idx := 0
	for _, g := range f.groups {
		prefixLen := len(strings.ToLower(g.Title) + " ")
		addedCount := 0
		for _, item := range g.Items {
			if m := matched[idx]; m != nil {
				idxs := []int{}
				for _, mIdx := range m.MatchedIndexes {
					// Adjusts removing provider name highlights
					if mIdx < prefixLen {
						continue
					}
					idxs = append(idxs, mIdx-prefixLen)
				}

				match := *m
				match.MatchedIndexes = idxs
				if addedCount == 0 {
					// Add section header
					g := g
					items = append(items, &g)
				}
				// Add the matched item
				item.SetMatch(match)
				items = append(items, item)
				addedCount++
			}
			idx++
		}
		if addedCount > 0 {
			// Add a space separator after each provider section
			items = append(items, list.NewSpacerItem(1))
		}
	}

	return items
}

// SetEmptyMessage sets the placeholder shown when the list has no items and
// no filter is active.
func (f *ModelsList) SetEmptyMessage(msg string) {
	f.emptyMessage = msg
}

// SetFilterEmptyMessage sets the placeholder shown when a filter is active but
// yields no matches.
func (f *ModelsList) SetFilterEmptyMessage(msg string) {
	f.filterEmptyMessage = msg
}

// Render renders the filterable list.
func (f *ModelsList) Render() string {
	if f.query != "" && f.filterEmptyMessage != "" {
		f.List.SetEmptyMessage(f.filterEmptyMessage)
	} else {
		f.List.SetEmptyMessage(f.emptyMessage)
	}
	return f.List.Render()
}

type modelGroups []ModelGroup

func (m modelGroups) Len() int {
	n := 0
	for _, g := range m {
		n += len(g.Items)
	}
	return n
}

func (m modelGroups) String(i int) string {
	count := 0
	for _, g := range m {
		if i < count+len(g.Items) {
			return g.Items[i-count].Filter()
		}
		count += len(g.Items)
	}
	return ""
}
