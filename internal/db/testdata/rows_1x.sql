-- One of each kind of row a 1.x install carries. Some timestamps use the
-- variable-width form 1.x releases wrote before normalization existed, chosen
-- so that ordering them as raw text gives the wrong earliest/latest event
-- (".5Z" sorts before "Z").
INSERT INTO jobs(id,input,type,storefront,title,artwork_url,artist_name,canonical_key,force,overrides,status,total_items,done_items,failed_items,error,created_at,updated_at) VALUES
	('j-done','https://music.apple.com/us/album/a/1','album','us','Album A','https://art/a.jpg','Artist A','album:us:1',1,'','completed',2,2,0,'','2024-07-10T12:00:00Z','2024-07-10T12:05:00.25Z'),
	('j-failed','https://music.apple.com/us/station/ra.1','station','us','Station','','','station:us:ra.1',0,'{"media_user_token":"tok","embed_cover":false}','failed',3,2,1,'1 of 3 tracks failed','2024-07-11T08:00:00.000000000Z','2024-07-11T08:10:00.000000000Z'),
	('j-running','https://music.apple.com/us/album/b/2','album','us','Album B','','','album:us:2',1,'{"force_overwrite":false,"embed_cover":true}','running',2,1,0,'','2024-07-12T09:00:00.000000000Z','2024-07-12T09:01:00.000000000Z'),
	('j-queued','https://music.apple.com/us/song/c/3','song','us','','','','song:us:3',0,'','queued',0,0,0,'','2024-07-12T10:00:00.000000000Z','2024-07-12T10:00:00.000000000Z'),
	('j-cancelled','https://music.apple.com/us/song/d/4','song','us','','','','song:us:4',0,'','cancelled',0,0,0,'cancelled','2024-07-12T11:00:00.000000000Z','2024-07-12T11:00:30.000000000Z'),
	('j-retried','https://music.apple.com/us/song/e/5','song','us','Song E','','','song:us:5',0,'','completed',1,1,0,'','2024-07-10T12:00:00.000000000Z','2024-07-10T12:00:02Z'),
	('j-refailed','https://music.apple.com/us/song/g/7','song','us','','','','song:us:7',0,'','failed',0,0,0,'failed again','2024-07-14T13:00:00.000000000Z','2024-07-14T13:00:05.5Z'),
	('j-corrupt','https://music.apple.com/us/song/f/6','song','us','','','','song:us:6',1,'{broken','completed',0,0,0,'','2024-07-13T00:00:00.000000000Z','2024-07-13T00:00:00.000000000Z'),
	('j-queued-early','https://music.apple.com/us/song/h/8','song','us','','','','song:us:8',0,'','queued',0,0,0,'','2024-07-12T08:30:00Z','2024-07-12T08:30:00Z');

INSERT INTO job_items(id,job_id,adam_id,kind,idx,title,artist,album,duration_ms,has_lyrics,lyrics_status,status,download_progress,decrypt_progress,progress_resolved,progress_remuxed,progress_verified,progress_tagged,progress_saved,codec,bit_depth,sample_rate,bitrate,file_size,output_path,error,created_at,updated_at) VALUES
	('i-done-1','j-done','11','song',1,'One','Artist A','Album A',180000,1,'fetched','completed',1,1,1,1,1,1,1,'alac',24,96000,0,40000000,'/music/Artist A/Album A/01 One.m4a','','2024-07-10T12:00:01Z','2024-07-10T12:02:00.5Z'),
	('i-done-2','j-done','12','song',2,'Two','Artist A','Album A',200000,0,'none','completed',1,1,1,1,0,1,1,'aac-lc',0,44100,256000,6000000,'/music/Artist A/Album A/02 Two.m4a','','2024-07-10T12:00:01.000000000Z','2024-07-10T12:04:00.000000000Z'),
	('i-failed-1','j-failed','21','song',1,'X','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,30000000,'/music/x.m4a','','2024-07-11T08:00:01.000000000Z','2024-07-11T08:05:00.000000000Z'),
	('i-failed-2','j-failed','22','song',2,'Y','','',0,0,'','failed',0.4,0,1,0,0,0,0,'alac',16,44100,0,0,'','decrypt failed','2024-07-11T08:00:01.000000000Z','2024-07-11T08:06:00.000000000Z'),
	('i-failed-3','j-failed','23','song',3,'Z','','',0,0,'','skipped_existing',0,0,1,0,0,0,0,'',0,0,0,1234,'/music/z.m4a','','2024-07-11T08:00:01.000000000Z','2024-07-11T08:07:00.000000000Z'),
	('i-running-1','j-running','31','song',1,'P','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',24,48000,0,1000,'/music/p.m4a','','2024-07-12T09:00:01.000000000Z','2024-07-12T09:00:30.000000000Z'),
	('i-running-2','j-running','32','song',2,'Q','','',0,0,'','downloading',0.5,0,1,0,0,0,0,'alac',24,48000,0,0,'','','2024-07-12T09:00:01.000000000Z','2024-07-12T09:00:40.000000000Z'),
	('i-retried-1','j-retried','51','song',1,'E','','',0,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,2000,'/music/e.m4a','','2024-07-10T12:00:00.000000000Z','2024-07-10T12:00:03.000000000Z');

INSERT INTO job_events(id,job_id,item_id,type,phase,message,payload,created_at) VALUES
	(1,'j-done','','job_queued','','job queued','{"id":"j-done","message":"job queued"}','2024-07-10T12:00:00Z'),
	(2,'j-done','','job_started','','job started','{"id":"j-done","message":"job started"}','2024-07-10T12:00:00.5Z'),
	(3,'j-done','i-done-1','item_progress','downloading','','{"id":"i-done-1","status":"downloading"}','2024-07-10T12:01:00Z'),
	(4,'j-done','i-done-1','item_completed','','done','{"id":"i-done-1","status":"completed"}','2024-07-10T12:02:00.5Z'),
	(5,'j-done','','job_finished','','completed','{"id":"j-done","status":"completed"}','2024-07-10T12:05:00.25Z'),
	(6,'j-done','','hook_succeeded','notify','','{"hook":"notify","attempt":1}','2024-07-10T12:05:01Z'),
	(7,'j-failed','','job_started','','job started','','2024-07-11T08:00:00.000000000Z'),
	(8,'j-failed','i-failed-2','item_progress','decrypting','','{"id":"i-failed-2"}','2024-07-11T08:05:30.000000000Z'),
	(9,'j-failed','i-failed-2','item_failed','','decrypt failed','{"id":"i-failed-2","status":"failed"}','2024-07-11T08:06:00.000000000Z'),
	(10,'j-failed','','job_failed','','1 of 3 tracks failed','{"id":"j-failed","status":"failed"}','2024-07-11T08:10:00.000000000Z'),
	(11,'j-running','','job_started','','job started','','2024-07-12T09:00:00.000000000Z'),
	(12,'j-running','i-running-2','item_progress','downloading','','{"id":"i-running-2","progress":{"download":0.5}}','2024-07-12T09:00:40.000000000Z'),
	(13,'j-queued','','job_queued','','job queued','','2024-07-12T10:00:00.000000000Z'),
	(14,'j-cancelled','','job_queued','','job queued','','2024-07-12T11:00:00.000000000Z'),
	(15,'j-cancelled','','job_cancelled','','job cancelled','','2024-07-12T11:00:30.000000000Z'),
	(16,'j-retried','','job_started','','job started','','2024-07-10T12:00:01Z'),
	(17,'j-retried','','job_failed','','failed','','2024-07-10T12:00:01.1Z'),
	(18,'j-retried','','job_retried','','retry','','2024-07-10T12:00:01.2Z'),
	(19,'j-retried','','job_started','','job started','','2024-07-10T12:00:01.5Z'),
	(20,'j-retried','','job_finished','','completed','','2024-07-10T12:00:02Z'),
	(21,'j-corrupt','','job_finished','','completed','','2024-07-13T00:00:00.000000000Z'),
	(22,'j-gone','','job_deleted','','','','2024-07-13T01:00:00.000000000Z'),
	(23,'j-refailed','','job_started','','job started','','2024-07-14T13:00:00Z'),
	(24,'j-refailed','','job_failed','','failed','','2024-07-14T13:00:05Z'),
	(25,'j-refailed','','job_retried','','retry','','2024-07-14T13:00:05.1Z'),
	(26,'j-refailed','','job_started','','job started','','2024-07-14T13:00:05.2Z'),
	(27,'j-refailed','','job_failed','','failed again','','2024-07-14T13:00:05.5Z');

INSERT INTO library_sync_anchor(position,song_id,song_name,album_name) VALUES
	(0,'i.song1','Newest','Album N'),
	(1,'i.song2','Older','Album O');

-- Playlists: one catalog playlist downloaded from two storefronts (the jp run
-- is newer and wins per song), a second playlist whose only file shares a
-- path with the first, and a library playlist (p.), which gets no record.
-- Albums: one album queued from two storefronts, a legacy four-segment key.
-- Songs: an album-track link queued. Artists: a queued legacy key. A queued
-- station and a queued library playlist, which keep only the run. Event 30 was handed out and deleted, so the next id is 31.
INSERT INTO jobs(id,input,type,storefront,title,artwork_url,curator_name,artwork_bg_color,canonical_key,force,overrides,status,total_items,error,created_at,updated_at) VALUES
	('j-pl-us','https://music.apple.com/us/playlist/mix/pl.u1','playlist','us','Mix','https://art/mix.jpg','Curator','112233','playlist:us:pl.u1',0,'','completed',3,'','2024-07-15T10:00:00Z','2024-07-15T10:10:00Z'),
	('j-pl-jp','https://music.apple.com/jp/playlist/mix/pl.u1','playlist','jp','Mix JP','https://art/mixjp.jpg','Curator','445566','playlist:jp:pl.u1',0,'','failed',3,'1 of 3 tracks failed','2024-07-16T10:00:00Z','2024-07-16T10:10:00Z'),
	('j-pl-other','https://music.apple.com/us/playlist/other/pl.u2','playlist','us','Other','','','','playlist:us:pl.u2',0,'','completed',1,'','2024-07-14T10:00:00Z','2024-07-14T10:10:00Z'),
	('j-pl-lib','https://music.apple.com/library/playlist/p.lib1','playlist','us','Favourites','','','','playlist:us:p.lib1',0,'','completed',1,'','2024-07-14T11:00:00Z','2024-07-14T11:10:00Z'),
	('j-album-us9','https://music.apple.com/us/album/n/9','album','us','','','','','album:us:9',0,'{"codec_alternative":false}','queued',0,'','2024-07-17T01:00:00Z','2024-07-17T01:00:00Z'),
	('j-album-jp9','https://music.apple.com/jp/album/n/9','album','jp','','','','','album:jp:9',0,'','queued',0,'','2024-07-17T02:00:00Z','2024-07-17T02:00:00Z'),
	('j-queued-song-album','https://music.apple.com/us/album/x/555?i=556','song','us','','','','','song:us:556',0,'','queued',0,'','2024-07-17T03:00:00Z','2024-07-17T03:00:00Z'),
	('j-legacy','https://music.apple.com/us/album/l/10','album','us','Legacy','','','','album:us:10:0123456789ab',0,'','completed',0,'','2024-07-09T00:00:00Z','2024-07-09T00:00:00Z'),
	('j-legacy-queued','https://music.apple.com/us/artist/a/20','artist','us','','','','','artist:us:20:abcdef012345',0,'','queued',0,'','2024-07-17T04:00:00Z','2024-07-17T04:00:00Z'),
	('j-station-queued','https://music.apple.com/us/station/s/ra.2','station','us','','','','','station:us:ra.2',0,'','queued',0,'','2024-07-17T05:00:00Z','2024-07-17T05:00:00Z'),
	('j-pl-lib-queued','https://music.apple.com/library/playlist/p.lib2','playlist','us','','','','','playlist:us:p.lib2',0,'','queued',0,'','2024-07-17T06:00:00Z','2024-07-17T06:00:00Z');

INSERT INTO job_items(id,job_id,adam_id,kind,idx,title,artist,album,duration_ms,has_lyrics,lyrics_status,status,download_progress,decrypt_progress,progress_resolved,progress_remuxed,progress_verified,progress_tagged,progress_saved,codec,bit_depth,sample_rate,bitrate,file_size,output_path,error,created_at,updated_at) VALUES
	('i-pl-us-1','j-pl-us','61','song',1,'A','Artist','Album',1000,1,'fetched','completed',1,1,1,1,1,1,1,'alac',16,44100,0,100,'/music/Playlists/Mix/01 A.m4a','','2024-07-15T10:00:01Z','2024-07-15T10:01:00Z'),
	('i-pl-us-2','j-pl-us','62','song',2,'B','Artist','Album',2000,0,'','skipped_existing',0,0,1,0,0,0,0,'',0,0,0,200,'/music/Playlists/Mix/02 B.m4a','','2024-07-15T10:00:01Z','2024-07-15T10:02:00Z'),
	('i-pl-us-3','j-pl-us','63','song',3,'C','Artist','Album',3000,0,'','failed',0,0,1,0,0,0,0,'',0,0,0,0,'','no manifest','2024-07-15T10:00:01Z','2024-07-15T10:03:00Z'),
	('i-pl-jp-1','j-pl-jp','61','song',1,'A','Artist','Album',1000,1,'fetched','completed',1,1,1,1,1,1,1,'aac-lc',0,44100,256000,50,'/music/Playlists/Mix JP/01 A.m4a','','2024-07-16T10:00:01Z','2024-07-16T10:01:00Z'),
	('i-pl-jp-2','j-pl-jp','62','song',2,'B','Artist','Album',2000,0,'','failed',0,0,1,0,0,0,0,'',0,0,0,0,'','decrypt failed','2024-07-16T10:00:01Z','2024-07-16T10:02:00Z'),
	('i-pl-jp-3','j-pl-jp','63','song',3,'C','Artist','Album',3000,0,'none','completed',1,1,1,1,1,1,1,'alac',24,96000,0,300,'/music/Playlists/Mix JP/03 C.m4a','','2024-07-16T10:00:01Z','2024-07-16T10:03:00Z'),
	('i-pl-other-1','j-pl-other','71','song',1,'O','Artist','Album',1000,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,400,'/music/Playlists/Mix/02 B.m4a','','2024-07-14T10:00:01Z','2024-07-14T10:01:00Z'),
	('i-pl-lib-1','j-pl-lib','81','song',1,'F','Artist','Album',1000,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,500,'/music/Playlists/Favourites/01 F.m4a','','2024-07-14T11:00:01Z','2024-07-14T11:01:00Z'),
	('i-legacy-1','j-legacy','101','song',1,'L','Artist','Legacy',1000,0,'','completed',1,1,1,1,1,1,1,'alac',16,44100,0,600,'/music/Artist/Legacy/01 L.m4a','','2024-07-09T00:00:01Z','2024-07-09T00:01:00Z');

INSERT INTO job_events(id,job_id,item_id,type,phase,message,payload,created_at) VALUES
	(28,'j-pl-us','','job_finished','','completed','','2024-07-15T10:10:00Z'),
	(29,'j-pl-jp','','job_failed','','1 of 3 tracks failed','','2024-07-16T10:10:00Z'),
	(30,'j-gone','','job_queued','','job queued','','2024-07-16T11:00:00Z');
DELETE FROM job_events WHERE id = 30;
