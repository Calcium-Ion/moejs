package main

func googleScenarios() []scenario {
	c := creds{baseURL: "https://generativelanguage.googleapis.com", apiKey: "google-test-api-key-0123456789abcdef"}
	const veo = "veo-3.0-generate-001"
	prompt := "Drone shot over a glacier lagoon at golden hour, icebergs glowing"
	opName := "models/veo-3.0-generate-001/operations/abc123def456"
	taskID := host.Base64URL(opName)
	request := obj{"model": veo, "prompt": prompt, "metadata": obj{"resolution": "1080p"}, "duration": 8, "size": "1920x1080"}
	clientBody := obj{"model": veo, "input": prompt, "seconds": 8, "size": "1920x1080", "resolution": "1080p"}
	i2vRequest := obj{"model": veo, "prompt": prompt, "metadata": obj{}, "images": arr{pngDataURL}, "duration": 8}
	done := obj{"name": opName, "done": true, "response": obj{"@type": "type.googleapis.com/google.ai.generativelanguage.v1beta.PredictLongRunningResponse", "generateVideoResponse": obj{"generatedVideos": arr{obj{"video": obj{"uri": "https://generativelanguage.googleapis.com/v1beta/files/abc:download?alt=media"}}}}}}
	view := taskView("google", "SUCCESS", "100%", "", done)

	return []scenario{
		{Name: "responses decode text-to-video", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(clientBody))}},
		{Name: "responses decode image-to-video data url", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(obj{"model": veo, "input": responsesInput([]string{prompt}, pngDataURL), "seconds": 8}))}},
		{Name: "responses decode rejects http image", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(obj{"model": veo, "input": prompt, "image": "https://img.example.com/a.png"}))}},
		{Name: "video decode json", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", jsonBody(obj{"model": veo, "prompt": prompt, "seconds": 6, "resolution": "720p"}))}},
		{Name: "video decode multipart with file", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"8"}, "metadata": arr{`{"resolution":"1080p"}`}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "frame.png", "mimeType": "image/png", "size": 12345}}))}},
		{Name: "video decode rejects seconds", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", jsonBody(obj{"model": veo, "prompt": prompt, "seconds": 5}))}},
		{Name: "build submit text-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo, userSetting: obj{"geminiVersion": "v1beta"}})}},
		{Name: "build submit image-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: veo})}},
		{Name: "parse submit operation", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo}), submitResp(200, obj{"name": opName})}},
		{Name: "parse submit done immediate", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo}), submitResp(200, done)}},
		{Name: "parse submit rejects missing name", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo}), submitResp(200, obj{"error": obj{"code": 400, "message": "Invalid argument"}})}},
		{Name: "usage facts", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo, usagePurpose: "facts"})}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo})}},
		{Name: "parse task in progress", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), obj{"name": opName}, pollResp(200)}},
		{Name: "parse task done", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), done, pollResp(200)}},
		{Name: "parse task error", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), obj{"name": opName, "done": true, "error": obj{"code": 3, "message": "Prompt violates usage guidelines"}}, pollResp(200)}},
		{Name: "usage on complete is null", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), taskInfo(taskID, "SUCCESS", "100%", ""), done}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(veo, "", clientBody, videoArtifacts()), view, nil}},
		{Name: "render events failure", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(veo, "", clientBody, nil), taskView("google", "FAILURE", "100%", "Prompt violates usage guidelines", nil), obj{"status": "IN_PROGRESS", "progress": 50}}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(veo, "", clientBody, videoArtifacts()), view}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, view}},
		{Name: "list artifacts", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, taskID, "SUCCESS", "text_to_video", done, nil, "")}},
		{Name: "build content request", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, taskID, "SUCCESS", "text_to_video", done, nil, "video")}},
	}
}
