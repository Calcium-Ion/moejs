package main

func klingScenarios() []scenario {
	c := creds{baseURL: "https://api.klingai.com", apiKey: "AKb2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7|SKf1e2d3c4b5a6978877665544332211"}
	gw := creds{baseURL: "https://gateway.example.com", apiKey: "sk-gw-0123456789abcdef", viaNewAPI: true}
	const v16 = "kling-v1-6"
	const v1 = "kling-v1"
	const v2 = "kling-v2-master"
	img := "https://img.example.com/reference/dancer.jpg"
	prompt := "A ballet dancer spinning on a rooftop at night, city lights bokeh"
	clientBody := obj{"model": v16, "input": prompt, "seconds": 10, "size": "1280x720", "mode": "pro"}
	request := obj{"model": v16, "prompt": prompt, "metadata": obj{"mode": "pro"}, "duration": 10, "size": "1280x720"}
	i2vRequest := obj{"model": v1, "prompt": prompt, "metadata": obj{"mode": "std"}, "image": img, "duration": 5, "size": "1024x1024"}
	succeed := obj{"code": 0, "message": "SUCCEED", "request_id": "req-kling-1", "data": obj{"task_id": "7381234567890123456", "task_status": "succeed", "task_status_msg": "", "created_at": 1700000000000, "updated_at": 1700000300000, "final_unit_deduction": "3.5", "task_result": obj{"videos": arr{obj{"id": "vid-1", "url": "https://cdn.klingai.com/video/vid-1.mp4?sig=abc", "duration": "5.1"}}}}}
	view := taskView("kling", "SUCCESS", "100%", "", succeed)

	return []scenario{
		{Name: "responses decode pro mode", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", v16, "", jsonBody(clientBody))}},
		{Name: "responses decode two images", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", v1, "", jsonBody(obj{"model": v1, "input": responsesInput([]string{prompt}, img, "https://img.example.com/reference/tail.jpg"), "seconds": 5}))}},
		{Name: "responses decode rejects std on v2", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", v2, "", jsonBody(obj{"model": v2, "input": prompt, "mode": "std"}))}},
		{Name: "video decode image", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", v1, "", jsonBody(obj{"model": v1, "prompt": prompt, "image": img, "seconds": 5}))}},
		{Name: "video decode rejects zero seconds", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", v1, "", jsonBody(obj{"model": v1, "prompt": prompt, "seconds": 0}))}},
		{Name: "video decode multipart file", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", v16, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"5"}, "mode": arr{"pro"}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "d.jpg", "mimeType": "image/jpeg", "size": 4096}}))}},
		{Name: "native decode submit", Hook: "native", Member: "decodeSubmit",
			Args: arr{routeCtx("POST", "/kling/v1/videos/text2video", nil, nil, jsonBody(obj{"model_name": v1, "prompt": prompt, "mode": "std", "duration": "5", "aspect_ratio": "16:9", "cfg_scale": 0.5}))}},
		{Name: "build submit vendor jwt", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16})}},
		{Name: "build submit image via gateway", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: gw, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: v1})}},
		{Name: "build submit rejects malformed key", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: creds{baseURL: c.baseURL, apiKey: "single-token"}, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16})}},
		{Name: "parse submit", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16}),
				submitResp(200, obj{"code": 0, "message": "SUCCEED", "request_id": "req-kling-1", "data": obj{"task_id": "7381234567890123456", "task_status": "submitted", "created_at": 1700000000000, "updated_at": 1700000000000}})}},
		{Name: "parse submit rejects code", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16}),
				submitResp(400, obj{"code": 1201, "message": "Invalid parameter: duration", "request_id": "req-kling-2"})}},
		{Name: "usage facts", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16, usagePurpose: "facts"})}},
		{Name: "usage billing ratios is null", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: v16, usagePurpose: "billing_ratios"})}},
		{Name: "build query jwt", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16})}},
		{Name: "parse task succeed with units", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16}), succeed, pollResp(200)}},
		{Name: "parse task failed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16}), obj{"code": 0, "message": "SUCCEED", "data": obj{"task_id": "7381234567890123456", "task_status": "failed", "task_status_msg": "content policy"}}, pollResp(200)}},
		{Name: "parse task unknown", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16}), obj{"code": 0, "data": obj{"task_id": "7381234567890123456", "task_status": "paused"}}, pollResp(200)}},
		{Name: "usage on complete units", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16}), taskInfo("7381234567890123456", "SUCCESS", "100%", ""), succeed}},
		{Name: "usage on complete null", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7381234567890123456", action: "text_to_video", model: v16}), taskInfo("7381234567890123456", "SUCCESS", "100%", ""), obj{"code": 0, "data": obj{"task_id": "x", "task_status": "succeed"}}}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(v16, "", clientBody, videoArtifacts()), view, nil}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(v16, "", clientBody, videoArtifacts()), view}},
		{Name: "native task status", Hook: "native", Member: "taskStatus",
			Args: arr{routeCtx("GET", "/kling/v1/videos/text2video/task_pub_0001", obj{"task_id": publicTaskID}, nil, nil), view}},
		{Name: "native task status without data", Hook: "native", Member: "taskStatus",
			Args: arr{routeCtx("GET", "/kling/v1/videos/text2video/task_pub_0001", obj{"task_id": publicTaskID}, nil, nil), taskView("kling", "IN_PROGRESS", "50%", "", nil)}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, view}},
		{Name: "build content request", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "7381234567890123456", "SUCCESS", "text_to_video", succeed, nil, "video")}},
	}
}
