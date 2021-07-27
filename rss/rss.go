package rss

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cblgh/plain/util"
	"os"
	"path/filepath"
	"strings"
)

// the rss spec, /abbreviated/
// channel [required]:
//    title
//    link
//    description
//
// item [required]:
//    title
//    link
//    description
//    pubDate

type FeedItem struct {
	RSSItem string
	Pubdate int64 // in unix time, for easy sortability
}

const RSS_STORE = "rss-store.json"

func OpenStore() map[string]FeedItem {
	b, err := os.ReadFile(RSS_STORE)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]FeedItem)
	}
	util.Check(err)
	var v map[string]FeedItem
	err = json.Unmarshal(b, &v)
	util.Check(err)
	return v
}

// structure of rss-store.json:
// {
//  {<listicleId>} : <FeedItem{rssItem, pubdate}>
//  ..
// }
func SaveStore(rssmap map[string]FeedItem) error {
	b, err := json.MarshalIndent(rssmap, "", "  ")
	if err != nil {
		return fmt.Errorf("save store: could not marshal map %w", err)
	}
	err = os.WriteFile(RSS_STORE, b, 0666)
	if err != nil {
		return fmt.Errorf("save store: could not save %s %w", RSS_STORE, err)
	}
	return nil
}

func SaveFeed(dest, name, feed string) error {
	err := os.WriteFile(filepath.Join(dest, name), []byte(feed), 0666)
	if err != nil {
		return fmt.Errorf("error when saving feed %s %w", name, err)
	}
	return nil
}

// Filters out a slice of <item> from a slice of FeedItem{}
func GetItems(items []FeedItem) []string {
	rssitems := make([]string, 0, len(items))
	for _, fi := range items {
		rssitems = append(rssitems, util.Indent(fi.RSSItem, "\t"))
	}
	return rssitems
}

const RSS_ITEM = `<item>
  <title>%s</title>
  <link>%s</link>
  <description>%s</description>
  <pubDate>%s</pubDate>
</item>`

func OutputRSSItem(pubdate, title, brief, link string) string {
	return fmt.Sprintf(RSS_ITEM, title, link, brief, pubdate)
}

const RSS_TEMPLATE = `<rss version="2.0">
  <channel>
    <title>%s</title>
    <link>%s</link>
    <description>%s</description>
%s
  </channel>
</rss>`

func OutputRSS(title, link, desc string, items []string) string {
	return fmt.Sprintf(RSS_TEMPLATE, title, link, desc, strings.Join(items, "\n"))
}
