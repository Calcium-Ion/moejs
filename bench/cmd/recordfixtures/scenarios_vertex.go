package main

func vertexScenarios() []scenario {
	c := creds{baseURL: "", oauth: obj{"authHeader": "Bearer ya29.a0AfB_byC-test-access-token", "projectId": "my-gcp-project-123"}}
	const veo = "veo-3.1-generate-preview"
	prompt := "Time-lapse of cherry blossoms opening along a Kyoto canal"
	opName := "projects/my-gcp-project-123/locations/us-central1/publishers/google/models/veo-3.1-generate-preview/operations/8f1e2d3c-4b5a-6978-8776-655443322110"
	taskID := host.Base64URL(opName)
	clientBody := obj{"model": veo, "input": prompt, "seconds": 8, "size": "1920x1080"}
	request := obj{"model": veo, "prompt": prompt, "metadata": obj{}, "duration": 8, "size": "1920x1080"}
	i2vRequest := obj{"model": veo, "prompt": prompt, "metadata": obj{"resolution": "1080p", "generateAudio": false}, "images": arr{pngDataURL}, "duration": 8}
	done := obj{"name": opName, "done": true, "response": obj{"@type": "type.googleapis.com/cloud.ai.large_models.vision.GenerateVideoResponse", "videos": arr{obj{"bytesBase64Encoded": "AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDE=", "mimeType": "video/mp4"}}}}
	view := taskView("vertex-ai", "SUCCESS", "100%", "", done)

	return []scenario{
		{Name: "responses decode text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(clientBody))}},
		{Name: "responses decode base64 image", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(obj{"model": veo, "input": responsesInput([]string{prompt}, pngDataURL), "resolution": "1080p"}))}},
		{Name: "responses decode rejects http image", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", veo, "", jsonBody(obj{"model": veo, "input": prompt, "images": arr{"https://img.example.com/a.png"}}))}},
		{Name: "video decode 4k", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", jsonBody(obj{"model": veo, "prompt": prompt, "seconds": 4, "resolution": "4k"}))}},
		{Name: "video decode multipart png", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"8"}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "a.png", "mimeType": "image/png", "size": 1234}}))}},
		{Name: "video decode rejects webp file", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", veo, "", multipartBody(obj{"prompt": arr{prompt}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "a.webp", "mimeType": "image/webp", "size": 1234}}))}},
		{Name: "build submit text regional", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo, userSetting: obj{"vertexRegion": "us-central1"}})}},
		{Name: "build submit image global", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: veo})}},
		{Name: "build submit rejects auth error", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo, authError: "decode oauth2_jwt credentials: invalid character"})}},
		{Name: "parse submit operation", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo}), submitResp(200, obj{"name": opName})}},
		{Name: "parse submit rejects missing name", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: veo}), submitResp(403, obj{"error": obj{"code": 403, "message": "Permission denied"}})}},
		{Name: "usage facts", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: veo, usagePurpose: "facts"})}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo})}},
		{Name: "build query rejects malformed id", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: host.Base64URL("operations/not-a-real-name"), action: "text_to_video", model: veo})}},
		{Name: "parse task done data url", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), done, pollResp(200)}},
		{Name: "parse task running", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), obj{"name": opName}, pollResp(200)}},
		{Name: "parse task error", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: taskID, action: "text_to_video", model: veo}), obj{"name": opName, "done": true, "error": obj{"code": 8, "message": "Quota exceeded"}}, pollResp(200)}},
		{Name: "render events success message", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(veo, "", clientBody, obj{}), view, nil}},
		{Name: "render events progress", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(veo, "", clientBody, nil), taskView("vertex-ai", "IN_PROGRESS", "50%", "", nil), obj{"status": "QUEUED", "progress": nil}}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(veo, "", clientBody, obj{}), view}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, view}},
		{Name: "list artifacts empty", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, taskID, "SUCCESS", "text_to_video", done, nil, "")}},
		{Name: "build content request rejects", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, taskID, "SUCCESS", "text_to_video", done, nil, "video")}},
	}
}
