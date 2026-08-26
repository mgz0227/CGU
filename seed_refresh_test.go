package main

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSeedAnnouncementRefreshesOnlyLegacyCopy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	announcement := store.announcements[len(store.announcements)-1]
	for _, item := range store.announcements {
		mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO cgu_announcements")).WithArgs(
			item.ID, item.TitleZh, item.TitleEn, item.ContentZh, item.ContentEn,
			item.Type, item.Audience, item.CourseID, item.PublishedAt, item.Published, item.Author,
		).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cgu_announcements SET title_zh = ?")).WithArgs(
		announcement.TitleZh, announcement.TitleEn, announcement.ContentZh, announcement.ContentEn, announcement.Type, announcement.Audience,
		announcement.CourseID, announcement.PublishedAt, announcement.Published, announcement.Author, announcement.ID,
		"7.0「无神怜爱的雪国」：至冬研究方向开放", "Version 7.0 “Everwinter Without Mercy”: Snezhnaya studies open",
		"根据原神官方 7.0 版本资讯，至冬成为新的旅途舞台。CGU 新增至冬研究与极地治理课程，官网同步提供官方新闻入口。",
		"Following the official Version 7.0 update, Snezhnaya is now the next stage of the journey. CGU adds a Snezhnaya studies track and links to the official news source.",
	).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.seedAnnouncementsLocked(context.Background()); err != nil {
		t.Fatalf("seed announcement refresh = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSeedSiteContentRefreshesOnlyLegacyDates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	store := NewStoreWithAdmin(testAdminUsername, testAdminPassword)
	store.db = db
	for _, item := range store.siteContent {
		if item == nil {
			continue
		}
		mock.ExpectExec(regexp.QuoteMeta("INSERT IGNORE INTO cgu_site_content")).WithArgs(item.Key, item.Zh, item.En).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	for _, key := range []string{"home.featureDate", "home.newsSnezhnayaDate"} {
		item := store.siteContent[key]
		mock.ExpectExec(regexp.QuoteMeta("UPDATE cgu_site_content SET zh_text = ?")).WithArgs(item.Zh, item.En, item.Key, "08.12", "08.12").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if err := store.seedSiteContentLocked(context.Background()); err != nil {
		t.Fatalf("seed site content refresh = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
