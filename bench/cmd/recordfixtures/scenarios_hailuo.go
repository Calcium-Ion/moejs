package main

func hailuoScenarios() []scenario {
	c := creds{baseURL: "https://api.minimax.chat", apiKey: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.minimax-test-token"}
	const h23 = "MiniMax-Hailuo-2.3"
	const h02 = "MiniMax-Hailuo-02"
	const h3 = "MiniMax-H3"
	img := "https://img.example.com/reference/first.jpg"
	img2 := "https://img.example.com/reference/last.jpg"
	prompt := "A lighthouse keeper walks along the cliff as a storm rolls in"
	clientBody := obj{"model": h23, "input": prompt, "seconds": 6, "size": "1080p"}
	request := obj{"model": h23, "prompt": prompt, "metadata": obj{"first_frame_image": img}, "duration": 6, "size": "1080p", "images": arr{img}}
	h3Request := obj{"model": h3, "prompt": prompt, "duration": 5, "metadata": obj{"resolution": "2K", "ratio": "16:9", "reference_video": "https://img.example.com/reference/ref.mp4", "aigc_watermark": false}}
	v1Success := obj{"task_id": "204312345678901234", "status": "Success", "file_id": "205312345678901234", "video_width": 1920, "video_height": 1080, "base_resp": obj{"status_code": 0, "status_msg": "success"}}
	h3Success := obj{"task": obj{"id": "h3-task-1", "status": "succeeded", "resolution": "2K", "content": obj{"url": "https://cdn.minimax.chat/video/h3.mp4?sig=abc"}, "usage": obj{"output_seconds": 5, "input_image_count": 2, "input_seconds": 0}}}

	return []scenario{
		{Name: "responses decode text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", h23, "", jsonBody(clientBody))}},
		{Name: "responses decode first and last frame", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", h02, "", jsonBody(obj{"model": h02, "input": responsesInput([]string{prompt}, img, img2), "seconds": 6}))}},
		{Name: "responses decode rejects missing input", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", h23, "", jsonBody(obj{"model": h23}))}},
		{Name: "video decode rejects 10s 1080P", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", h23, "", jsonBody(obj{"model": h23, "prompt": prompt, "seconds": 10, "resolution": "1080P"}))}},
		{Name: "video decode image 10s 512P", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", h02, "", jsonBody(obj{"model": h02, "prompt": prompt, "seconds": 10, "resolution": "512P", "input_reference": img}))}},
		{Name: "video decode multipart file", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", h02, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"6"}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "f.jpg", "mimeType": "image/jpeg", "size": 2048}}))}},
		{Name: "build submit v1 image-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "image_to_video", model: h23})}},
		{Name: "build submit H3 reference video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: h3Request, action: "text_to_video", model: h3})}},
		{Name: "build submit H3 rejects duration", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": h3, "prompt": prompt, "duration": 3}, action: "text_to_video", model: h3})}},
		{Name: "parse submit v1", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "image_to_video", model: h23}),
				submitResp(200, obj{"task_id": "204312345678901234", "base_resp": obj{"status_code": 0, "status_msg": "success"}})}},
		{Name: "parse submit v1 rejects base_resp", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "image_to_video", model: h23}),
				submitResp(200, obj{"task_id": "", "base_resp": obj{"status_code": 1002, "status_msg": "rate limit exceeded"}})}},
		{Name: "parse submit H3 rejects api error", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: h3Request, action: "text_to_video", model: h3}),
				submitResp(429, obj{"error": obj{"http_code": 429, "message": "Too many requests"}})}},
		{Name: "usage facts v1", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "image_to_video", model: h23, usagePurpose: "facts"})}},
		{Name: "usage facts H3", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: h3Request, action: "text_to_video", model: h3, usagePurpose: "facts"})}},
		{Name: "build query v1", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "204312345678901234", action: "image_to_video", model: h23})}},
		{Name: "build query H3", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "h3-task-1", action: "text_to_video", model: h3})}},
		{Name: "parse task v1 success", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "204312345678901234", action: "image_to_video", model: h23}), v1Success, pollResp(200)}},
		{Name: "parse task v1 processing", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "204312345678901234", action: "image_to_video", model: h23}), obj{"task_id": "204312345678901234", "status": "Processing", "base_resp": obj{"status_code": 0, "status_msg": "success"}}, pollResp(200)}},
		{Name: "parse task H3 success", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "h3-task-1", action: "text_to_video", model: h3}), h3Success, pollResp(200)}},
		{Name: "parse task H3 server error throws", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "h3-task-1", action: "text_to_video", model: h3}), obj{"error": obj{"http_code": 503, "message": "service unavailable"}}, pollResp(503)}},
		{Name: "usage on complete v1 resolution", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "204312345678901234", action: "image_to_video", model: h23}), taskInfo("204312345678901234", "SUCCESS", "100%", ""), v1Success}},
		{Name: "usage on complete H3", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "h3-task-1", action: "text_to_video", model: h3}), taskInfo("h3-task-1", "SUCCESS", "100%", ""), h3Success}},
		{Name: "render events failure", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(h23, "", clientBody, nil), taskView("hailuo", "FAILURE", "100%", "task failed", nil), nil}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(h23, "", clientBody, videoArtifacts()), taskView("hailuo", "SUCCESS", "100%", "", v1Success)}},
		{Name: "video render base_resp error", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, taskView("hailuo", "FAILURE", "100%", "", obj{"base_resp": obj{"status_code": 1026, "status_msg": "sensitive content"}})}},
		{Name: "list artifacts file id", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "204312345678901234", "SUCCESS", "image_to_video", v1Success, nil, "")}},
		{Name: "build content request file download", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "204312345678901234", "SUCCESS", "image_to_video", v1Success, nil, "video")}},
	}
}
