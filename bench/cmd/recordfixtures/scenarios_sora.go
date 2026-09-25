package main

func soraScenarios() []scenario {
	c := creds{baseURL: "https://api.openai.com", apiKey: "sk-proj-0123456789abcdefghijklmnopqrstuvwxyz0123456789"}
	const sora2 = "sora-2"
	img := "https://img.example.com/reference/corgi.png"
	prompt := "A corgi surfing a huge wave at sunset, golden light, cinematic"
	clientBody := obj{"model": sora2, "input": responsesInput([]string{prompt, "Keep the palette warm."}), "seconds": 8, "size": "1280x720", "metadata": obj{"trace": "t1"}}
	request := obj{"model": sora2, "prompt": prompt + "\nKeep the palette warm.", "seconds": 8, "size": "1280x720", "metadata": obj{"trace": "t1"}}
	video := obj{"id": "video_68a1b2c3d4e5f6", "object": "video", "model": sora2, "status": "queued", "progress": 0, "created_at": 1700000000, "size": "1280x720", "seconds": "8"}
	completed := obj{"id": "video_68a1b2c3d4e5f6", "object": "video", "model": sora2, "status": "completed", "progress": 100, "created_at": 1700000000, "completed_at": 1700000300, "size": "1280x720", "seconds": "8"}
	view := taskView("sora", "SUCCESS", "100%", "", completed)

	return []scenario{
		{Name: "responses decode text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", sora2, "", jsonBody(clientBody))}},
		{Name: "responses decode image reference", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", sora2, "", jsonBody(obj{"model": sora2, "input": responsesInput([]string{prompt}, img), "duration": 4}))}},
		{Name: "responses decode rejects missing input", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", sora2, "", jsonBody(obj{"model": sora2, "metadata": obj{}}))}},
		{Name: "video decode json", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", sora2, "", jsonBody(obj{"model": sora2, "prompt": prompt, "seconds": "4", "size": "720x1280", "input_reference": img}))}},
		{Name: "video decode multipart", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", sora2, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"8"}, "size": arr{"1280x720"}, "metadata": arr{`{"trace":"t2"}`}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "ref.png", "mimeType": "image/png", "size": 2048}}))}},
		{Name: "video decode rejects seconds", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", sora2, "", jsonBody(obj{"model": sora2, "prompt": prompt, "seconds": 5000}))}},
		{Name: "build submit json", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: sora2})}},
		{Name: "build submit multipart with file", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, protocol: "openai_video", clientBody: obj{}, requestBody: obj{"model": sora2, "prompt": prompt, "seconds": 8, "size": "1280x720", "metadata": obj{"trace": "t2"}}, action: "image_to_video", model: sora2,
				files: arr{obj{"ref": "request_file:input_reference:0", "field": "input_reference", "filename": "ref.png", "mimeType": "image/png", "size": 2048}}})}},
		{Name: "build submit remix", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"prompt": "make it snow"}, action: "remix", model: sora2, originTaskID: "video_origin_1"})}},
		{Name: "build submit rejects empty prompt", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": sora2, "prompt": "   "}, action: "text_to_video", model: sora2})}},
		{Name: "parse submit", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: sora2}), submitResp(200, video)}},
		{Name: "parse submit rejects empty id", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: sora2}), submitResp(200, obj{"error": obj{"message": "bad"}})}},
		{Name: "usage facts", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: sora2, usagePurpose: "facts"})}},
		{Name: "usage remix empty", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"prompt": "make it snow"}, action: "remix", model: sora2, usagePurpose: "facts"})}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "video_68a1b2c3d4e5f6", action: "text_to_video", model: sora2})}},
		{Name: "parse task in progress", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "video_68a1b2c3d4e5f6", action: "text_to_video", model: sora2}), obj{"id": "video_68a1b2c3d4e5f6", "status": "in_progress", "progress": 42}, pollResp(200)}},
		{Name: "parse task completed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "video_68a1b2c3d4e5f6", action: "text_to_video", model: sora2}), completed, pollResp(200)}},
		{Name: "parse task failed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "video_68a1b2c3d4e5f6", action: "text_to_video", model: sora2}), obj{"id": "video_68a1b2c3d4e5f6", "status": "failed", "error": obj{"code": "moderation_blocked", "message": "Your request was blocked by our moderation system."}}, pollResp(200)}},
		{Name: "usage on complete", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "video_68a1b2c3d4e5f6", action: "text_to_video", model: sora2}), taskInfo("video_68a1b2c3d4e5f6", "SUCCESS", "100%", ""), completed}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(sora2, "", clientBody, videoArtifacts()), view, obj{"status": "IN_PROGRESS", "progress": 42}}},
		{Name: "render events progress", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(sora2, "", clientBody, nil), taskView("sora", "IN_PROGRESS", "42%", "", nil), nil}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(sora2, "", clientBody, videoArtifacts()), view}},
		{Name: "video render with data", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, view}},
		{Name: "video render legacy", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, taskView("sora", "FAILURE", "100%", "blocked", nil)}},
		{Name: "list artifacts", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "video_68a1b2c3d4e5f6", "SUCCESS", "text_to_video", completed, nil, "")}},
		{Name: "build content request", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "video_68a1b2c3d4e5f6", "SUCCESS", "text_to_video", completed, nil, "video")}},
	}
}
