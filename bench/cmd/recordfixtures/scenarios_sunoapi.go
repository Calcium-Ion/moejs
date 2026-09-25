package main

func sunoapiScenarios() []scenario {
	c := creds{baseURL: "https://suno.example.com", apiKey: "sk-suno-0123456789abcdef"}
	const music = "suno_music"
	const lyrics = "suno_lyrics"
	prompt := "An upbeat jazz song about morning coffee in Lisbon"
	clientBody := obj{"model": music, "input": prompt, "metadata": obj{"mv": "chirp-v4", "tags": "jazz, upbeat", "title": "Lisbon Mornings"}}
	request := obj{"mv": "chirp-v4", "tags": "jazz, upbeat", "title": "Lisbon Mornings", "gpt_description_prompt": prompt}
	songs := arr{
		obj{"id": "song-aaa111", "title": "Lisbon Mornings", "text": "[Verse]\nSteam rises from the cup\nTram bells ring the morning up", "audio_url": "https://cdn.suno.ai/song-aaa111.mp3", "image_url": "https://cdn.suno.ai/image_song-aaa111.jpeg", "video_url": "https://cdn.suno.ai/song-aaa111.mp4", "status": "complete", "model_name": "chirp-v4", "duration": 187.2},
		obj{"id": "song-bbb222", "title": "Lisbon Mornings", "text": "[Verse]\nSunlight on the tiles", "audio_url": "https://cdn.suno.ai/song-bbb222.mp3", "image_url": "https://cdn.suno.ai/image_song-bbb222.jpeg", "status": "complete", "model_name": "chirp-v4", "duration": 201.4},
	}
	batchItem := obj{"task_id": "suno-task-1", "action": "MUSIC", "status": "SUCCESS", "fail_reason": "", "submit_time": 1700000000, "start_time": 1700000005, "finish_time": 1700000190, "progress": "100%", "data": songs}
	audioKey := "audio-" + host.HmacSHA256("song-aaa111", "new-api:suno:artifact-key")
	coverKey := "cover-" + host.HmacSHA256("song-bbb222", "new-api:suno:artifact-key")
	audioKey2 := "audio-" + host.HmacSHA256("song-bbb222", "new-api:suno:artifact-key")
	artifacts := obj{
		audioKey:  obj{"key": audioKey, "type": "audio", "mimeType": "audio/mpeg", "url": artifactURL(audioKey)},
		audioKey2: obj{"key": audioKey2, "type": "audio", "mimeType": "audio/mpeg", "url": artifactURL(audioKey2)},
	}
	view := taskView("sunoapi", "SUCCESS", "100%", "", songs)
	taskCtx := queryCtx(queryOpts{creds: c, taskID: "suno-task-1", action: "MUSIC", model: music})

	return []scenario{
		{Name: "responses decode music", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", music, "", jsonBody(clientBody))}},
		{Name: "responses decode lyrics", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", lyrics, "", jsonBody(obj{"model": lyrics, "input": responsesInput([]string{"a lullaby about the sea"})}))}},
		{Name: "responses decode rejects model", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", "suno_video", "", jsonBody(obj{"model": "suno_video", "input": prompt}))}},
		{Name: "native decode submit", Hook: "native", Member: "decodeSubmit",
			Args: arr{routeCtx("POST", "/suno/submit/music", obj{"action": "music"}, nil, jsonBody(obj{"prompt": prompt, "mv": "chirp-v4", "make_instrumental": false}))}},
		{Name: "native decode batch", Hook: "native", Member: "decodeBatch",
			Args: arr{routeCtx("POST", "/suno/fetch", nil, nil, jsonBody(obj{"ids": arr{"suno-task-1", "suno-task-2"}}))}},
		{Name: "native decode rejects action", Hook: "native", Member: "decodeSubmit",
			Args: arr{routeCtx("POST", "/suno/submit/video", obj{"action": "video"}, nil, jsonBody(obj{"prompt": prompt}))}},
		{Name: "build submit music", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "MUSIC", model: music})}},
		{Name: "build submit rejects empty lyrics prompt", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{}, action: "LYRICS", model: lyrics})}},
		{Name: "parse submit", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "MUSIC", model: music}), submitResp(200, obj{"code": "success", "message": "", "data": "suno-task-1"})}},
		{Name: "parse submit rejects failure", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "MUSIC", model: music}), submitResp(200, obj{"code": "fail", "message": "insufficient credits", "data": nil})}},
		{Name: "usage facts music", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "MUSIC", model: music, usagePurpose: "facts"})}},
		{Name: "usage billing ratios is null", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "MUSIC", model: music, usagePurpose: "billing_ratios"})}},
		{Name: "build batch query", Hook: "buildBatchQueryRequest",
			Args: arr{batchCtx(c, arr{taskCtx, queryCtx(queryOpts{creds: c, taskID: "suno-task-2", action: "LYRICS", model: lyrics})}), arr{taskCtx, queryCtx(queryOpts{creds: c, taskID: "suno-task-2", action: "LYRICS", model: lyrics})}}},
		{Name: "build query single", Hook: "buildQueryRequest", Args: arr{taskCtx}},
		{Name: "parse batch result", Hook: "parseBatchResult",
			Args: arr{batchCtx(c, arr{taskCtx}), obj{"code": "success", "message": "", "data": arr{batchItem, obj{"task_id": "suno-task-2", "action": "LYRICS", "status": "IN_PROGRESS", "progress": "40%"}}}, pollResp(200)}},
		{Name: "parse batch rejects failure", Hook: "parseBatchResult",
			Args: arr{batchCtx(c, arr{taskCtx}), obj{"code": "fail", "message": "unauthorized"}, pollResp(401)}},
		// parseTaskResult is not recorded: the plugin wraps the body as {code: 200, data: [body]}
		// and parseBatchResult then throws with an empty message (code !== "success"), which the
		// new-api fixture format cannot express (expectedError must be non-empty). Suno polls in batch mode.
		{Name: "usage on complete music", Hook: "extractUsageOnComplete", Args: arr{taskCtx, taskInfo("suno-task-1", "SUCCESS", "100%", ""), songs}},
		{Name: "usage on complete lyrics", Hook: "extractUsageOnComplete", Args: arr{taskCtx, taskInfo("suno-task-2", "SUCCESS", "100%", ""), obj{"id": "lyr-1", "text": "la la la", "title": "Lullaby"}}},
		{Name: "list artifacts", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "suno-task-1", "SUCCESS", "MUSIC", songs, nil, "")}},
		{Name: "build content request cover", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "suno-task-1", "SUCCESS", "MUSIC", songs, nil, coverKey)}},
		{Name: "build content request rejects unknown", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "suno-task-1", "SUCCESS", "MUSIC", songs, nil, "audio-deadbeef")}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(music, "", clientBody, artifacts), view, nil}},
		{Name: "render final music", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(music, "", clientBody, artifacts), view}},
		{Name: "render final lyrics", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(lyrics, "", obj{"model": lyrics}, obj{}), taskView("sunoapi", "SUCCESS", "100%", "", arr{obj{"id": "lyr-1", "text": "la la la", "title": "Lullaby"}})}},
		{Name: "native render submit", Hook: "native", Member: "renderSubmit",
			Args: arr{routeCtx("POST", "/suno/submit/music", obj{"action": "music"}, nil, jsonBody(obj{})), taskView("sunoapi", "SUBMITTED", "", "", obj{"message": "submitted"})}},
		{Name: "native render tasks", Hook: "native", Member: "renderTasks",
			Args: arr{routeCtx("POST", "/suno/fetch", nil, nil, jsonBody(obj{"ids": arr{publicTaskID}})), arr{view, taskView("sunoapi", "IN_PROGRESS", "40%", "", nil)}}},
	}
}
