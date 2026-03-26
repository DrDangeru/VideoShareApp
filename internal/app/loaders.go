package app

func (s *Server) loadSimilarVideos(
	videoID, channelID int64,
	location string,
	user *User,
) ([]SimilarVideo, error) {
	var query string
	var args []any

	if user != nil && user.IsAdmin {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ?
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{user.ID, user.ID, videoID, channelID, location, channelID}
	} else if user != nil {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ? and v.is_public = 1 and v.is_admin_locked = 0
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{user.ID, user.ID, videoID, channelID, location, channelID}
	} else {
		query = `
			select v.id, v.title, v.description, v.media_url, v.thumbnail_url,
				v.location, c.id, c.name, 0, 0, v.is_public
			from videos v join channels c on c.id = v.channel_id
			where v.id != ? and v.is_public = 1 and v.is_admin_locked = 0
				and (v.channel_id = ? or v.location = ?)
			order by case when v.channel_id = ? then 0 else 1 end, v.id desc
			limit 8
		`
		args = []any{videoID, channelID, location, channelID}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []SimilarVideo
	for rows.Next() {
		var item SimilarVideo
		err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Description,
			&item.MediaURL,
			&item.ThumbnailURL,
			&item.Location,
			&item.ChannelID,
			&item.ChannelName,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadSimilarChannels(
	excludeChannelID int64,
	location string,
	user *User,
) ([]SimilarChannel, error) {
	var query string
	var args []any

	if user != nil && user.IsAdmin {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url,
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{user.ID, excludeChannelID, location}
	} else if user != nil {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url,
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{user.ID, excludeChannelID, location}
	} else {
		query = `
			select c.id, c.name, c.location, c.avatar_url, v.id, v.title, v.media_url, 0
			from channels c
			join videos v on v.id = (
				select v2.id from videos v2
				where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
				order by v2.id desc limit 1
			)
			where c.id != ? and c.location = ?
			order by v.id desc
			limit 6
		`
		args = []any{excludeChannelID, location}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []SimilarChannel
	for rows.Next() {
		var item SimilarChannel
		err := rows.Scan(
			&item.ChannelID,
			&item.ChannelName,
			&item.Location,
			&item.AvatarURL,
			&item.LatestVideoID,
			&item.LatestVideoTitle,
			&item.LatestVideoURL,
			&item.IsSubscribed,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadChannelSidebar(user *User, subscribedOnly bool) ([]ChannelSidebarItem, error) {
	args := []any{}
	query := `
		select
			c.id,
			c.name,
			c.location,
			c.avatar_url,
			v.id,
			v.title,
			v.media_url,
			0
		from channels c
		join videos v on v.id = (
			select v2.id
			from videos v2
			where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
			order by v2.id desc
			limit 1
		)
	`

	if user != nil {
		if user.IsAdmin {
			query = `
				select
					c.id,
					c.name,
					c.location,
					c.avatar_url,
					v.id,
					v.title,
					v.media_url,
					exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
				from channels c
				join videos v on v.id = (
					select v2.id
					from videos v2
					where v2.channel_id = c.id
					order by v2.id desc
					limit 1
				)
			`
			args = append(args, user.ID)
		} else {
			query = `
				select
					c.id,
					c.name,
					c.location,
					c.avatar_url,
					v.id,
					v.title,
					v.media_url,
					exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id)
				from channels c
				join videos v on v.id = (
					select v2.id
					from videos v2
					where v2.channel_id = c.id and v2.is_public = 1 and v2.is_admin_locked = 0
					order by v2.id desc
					limit 1
				)
			`
			args = append(args, user.ID)
		}
	}

	if subscribedOnly {
		if user == nil {
			return []ChannelSidebarItem{}, nil
		}
		query += `
			where exists(
				select 1 from subscriptions s
				where s.user_id = ? and s.channel_id = c.id
			)`
		args = append(args, user.ID)
	}

	if user != nil {
		query += `
			order by case when c.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.Location)
	} else {
		query += `
			order by v.id desc
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ChannelSidebarItem{}
	for rows.Next() {
		var item ChannelSidebarItem
		err := rows.Scan(
			&item.ChannelID,
			&item.ChannelName,
			&item.Location,
			&item.AvatarURL,
			&item.LatestVideoID,
			&item.LatestVideoTitle,
			&item.LatestVideoURL,
			&item.IsSubscribed,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Server) loadFeed(user *User) ([]VideoView, error) {
	args := []any{}
	query := `
		select
			v.id,
			v.channel_id,
			v.title,
			v.description,
			v.media_url,
			v.thumbnail_url,
			v.location,
			c.name,
			0,
			0,
			v.is_public,
			v.is_admin_locked,
			v.allow_comments
		from videos v
		join channels c on c.id = v.channel_id
		where v.is_public = 1 and v.is_admin_locked = 0
	`

	if user != nil && user.IsAdmin {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public,
				v.is_admin_locked,
				v.allow_comments
			from videos v
			join channels c on c.id = v.channel_id
			order by case when v.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.ID, user.ID, user.Location)
	} else if user != nil {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				exists(select 1 from favorites f where f.user_id = ? and f.video_id = v.id),
				exists(select 1 from subscriptions s where s.user_id = ? and s.channel_id = c.id),
				v.is_public,
				v.is_admin_locked,
				v.allow_comments
			from videos v
			join channels c on c.id = v.channel_id
			where v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
			order by case when v.location = ? then 0 else 1 end, v.id desc
		`
		args = append(args, user.ID, user.ID, user.Location)
	} else {
		query = `
			select
				v.id,
				v.channel_id,
				v.title,
				v.description,
				v.media_url,
				v.thumbnail_url,
				v.location,
				c.name,
				0,
				0,
				v.is_public,
				v.is_admin_locked,
				v.allow_comments
			from videos v
			join channels c on c.id = v.channel_id
			where v.is_public = 1 and v.is_admin_locked = 0 and c.is_admin_locked = 0
			order by v.id desc
		`
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := []VideoView{}
	for rows.Next() {
		var item VideoView
		err := rows.Scan(
			&item.ID,
			&item.ChannelID,
			&item.Title,
			&item.Description,
			&item.MediaURL,
			&item.ThumbnailURL,
			&item.Location,
			&item.ChannelName,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
			&item.IsAdminLocked,
			&item.AllowComments,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, item)
	}

	return videos, rows.Err()
}

func (s *Server) loadUserVideos(user *User) ([]VideoView, error) {
	query := `
		select
			v.id,
			v.channel_id,
			v.title,
			v.description,
			v.media_url,
			v.thumbnail_url,
			v.location,
			c.name,
			0,
			0,
			v.is_public,
			v.is_admin_locked,
			v.allow_comments
		from videos v
		join channels c on c.id = v.channel_id
		where c.user_id = ?
		order by v.id desc
	`
	args := []any{user.ID}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []VideoView
	for rows.Next() {
		var item VideoView
		err := rows.Scan(
			&item.ID,
			&item.ChannelID,
			&item.Title,
			&item.Description,
			&item.MediaURL,
			&item.ThumbnailURL,
			&item.Location,
			&item.ChannelName,
			&item.IsFavorite,
			&item.IsSubscribed,
			&item.IsPublic,
			&item.IsAdminLocked,
			&item.AllowComments,
		)
		if err != nil {
			return nil, err
		}
		videos = append(videos, item)
	}

	return videos, rows.Err()
}

func (s *Server) loadManageableChannels(user *User) ([]Channel, error) {
	query := `select id, name, location, user_id from channels where user_id = ? order by name`
	args := []any{user.ID}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Location, &ch.OwnerUserID); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *Server) loadComments(videoID int64) ([]Comment, error) {
	rows, err := s.db.Query(`
		select c.id, c.video_id, c.user_id, c.content, c.created_at, u.email
		from comments c
		join users u on u.id = c.user_id
		where c.video_id = ?
		order by c.created_at desc
	`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.VideoID, &c.UserID, &c.Content, &c.CreatedAt, &c.UserEmail); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
