package main

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// A public terminal snapshot does not approve an outbound publication. Only an
// item already accepted by the collector's existing pipeline can claim such a
// cached row; otherwise normal duplicate handling is unchanged.
func claimTerminalNewsItem(ctx context.Context, tx pgx.Tx, item NewsItem) (bool, error) {
	if !item.PublicationEligible {
		return false, nil
	}
	entities, err := json.Marshal(item.Entities)
	if err != nil {
		return false, err
	}
	assets, err := json.Marshal(item.Assets)
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `UPDATE news_items SET
	 category=$1,title=$2,source=$3,published_at=$4,summary=NULLIF($5,''),image=NULLIF($6,''),calendar_only=$7,
	 original_title=$8,original_text=$9,original_language=$10,translated_title=NULLIF($11,''),translated_summary=NULLIF($12,''),
	 source_id=$13,source_type=$14,event_at=$15,event_type=$16,entities=$17::jsonb,assets=$18::jsonb,publication_eligible=TRUE
	 WHERE (url=$19 OR ($20<>'' AND url_hash=$20)) AND publication_eligible=FALSE
	 AND COALESCE(to_jsonb(news_items)->>'terminal_collected','false')='true'`,
		item.Category, item.Title, item.Source, item.Published, item.Summary, item.Image, item.CalendarOnly,
		item.OriginalTitle, item.OriginalText, item.OriginalLanguage, item.TranslatedTitle, item.TranslatedSummary,
		item.SourceID, item.SourceType, item.EventAt, item.EventType, string(entities), string(assets), item.URL, item.URLHash)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}
