package main

func jimengScenarios() []scenario {
	c := creds{baseURL: "https://visual.volcengineapi.com", apiKey: "AKLTYjAxMjM0NTY3ODkwYWJjZGVm|WVdKalpHVm1NREV5TXpRMU5qYzRPVEF4TWpNME5UWTM"}
	gw := creds{baseURL: "https://gateway.example.com", apiKey: "sk-gw-0123456789abcdef", viaNewAPI: true}
	const l20 = "jimeng_vgfm_t2v_l20"
	img := "https://img.example.com/reference/temple.jpg"
	prompt := "古风少女在雨中撑着油纸伞走过石桥，镜头缓慢推进"
	clientBody := obj{"model": l20, "input": prompt, "seconds": 5, "size": "16:9"}
	request := obj{"model": l20, "prompt": prompt, "metadata": obj{}, "duration": 5}
	i2vRequest := obj{"model": l20, "prompt": prompt, "metadata": obj{}, "duration": 5, "images": arr{img}}
	done := obj{"code": 10000, "data": obj{"status": "done", "video_url": "https://p9-aiop-sign.byteimg.com/video/abc.mp4?x-expires=1700003600&x-signature=xyz"}, "message": "Success", "request_id": "2023111422132001", "status": 10000, "time_elapsed": "1.2s"}
	view := taskView("jimeng", "SUCCESS", "100%", "", done)

	return []scenario{
		{Name: "responses decode text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", l20, "", jsonBody(clientBody))}},
		{Name: "responses decode rejects image input", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", l20, "", jsonBody(obj{"model": l20, "input": responsesInput([]string{prompt}, img)}))}},
		{Name: "video decode image-to-video", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", l20, "", jsonBody(obj{"model": l20, "prompt": prompt, "image": img, "seconds": 5}))}},
		{Name: "video decode rejects 10s on l20", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", l20, "", jsonBody(obj{"model": l20, "prompt": prompt, "seconds": 10}))}},
		{Name: "native decode submit", Hook: "native", Member: "decodeRequest",
			Args: arr{routeCtx("POST", "/jimeng/", nil, obj{"Action": arr{"CVSync2AsyncSubmitTask"}, "Version": arr{"2022-08-31"}}, jsonBody(obj{"req_key": l20, "prompt": prompt, "aspect_ratio": "16:9", "seed": -1}))}},
		{Name: "native decode query", Hook: "native", Member: "decodeRequest",
			Args: arr{routeCtx("POST", "/jimeng/", nil, obj{"Action": arr{"CVSync2AsyncGetResult"}, "Version": arr{"2022-08-31"}}, jsonBody(obj{"req_key": l20, "task_id": "7312345678901234567"}))}},
		{Name: "native decode rejects action", Hook: "native", Member: "decodeRequest",
			Args: arr{routeCtx("POST", "/jimeng/", nil, obj{"Action": arr{"CVProcess"}}, jsonBody(obj{"req_key": l20}))}},
		{Name: "build submit signed vendor", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: l20})}},
		{Name: "build submit image via gateway", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: gw, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: l20})}},
		{Name: "build submit v30 frames", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: gw, clientBody: obj{}, requestBody: obj{"model": "jimeng_v30_1080p", "prompt": prompt, "metadata": obj{"frames": 241}, "size": "1920x1080"}, action: "text_to_video", model: "jimeng_v30_1080p"})}},
		{Name: "build submit rejects malformed key", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: creds{baseURL: c.baseURL, apiKey: "no-separator-key"}, clientBody: clientBody, requestBody: request, action: "text_to_video", model: l20})}},
		{Name: "parse submit", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: l20}),
				submitResp(200, obj{"code": 10000, "data": obj{"task_id": "7312345678901234567"}, "message": "Success", "request_id": "2023111422132001", "status": 10000, "time_elapsed": "0.3s"})}},
		{Name: "parse submit rejects code", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: l20}),
				submitResp(400, obj{"code": 50411, "message": "Input image content does not meet the requirements", "request_id": "2023111422132002", "status": 50411})}},
		{Name: "usage facts s2 pro", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: l20, usagePurpose: "facts"})}},
		{Name: "usage facts v30 1080p 10s", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": "jimeng_v30_1080p", "prompt": prompt, "metadata": obj{"frames": 241}}, action: "text_to_video", model: "jimeng_v30_1080p", usagePurpose: "facts"})}},
		{Name: "build query signed", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7312345678901234567", action: "image_to_video", model: l20, state: obj{"req_key": "jimeng_vgfm_i2v_l20"}})}},
		{Name: "parse task done", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7312345678901234567", action: "text_to_video", model: l20}), done, pollResp(200)}},
		{Name: "parse task in queue", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7312345678901234567", action: "text_to_video", model: l20}), obj{"code": 10000, "data": obj{"status": "in_queue"}, "message": "Success", "status": 10000}, pollResp(200)}},
		{Name: "parse task failure code", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7312345678901234567", action: "text_to_video", model: l20}), obj{"code": 50429, "message": "Rate limit exceeded", "status": 50429}, pollResp(200)}},
		{Name: "usage on complete is null", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "7312345678901234567", action: "text_to_video", model: l20}), taskInfo("7312345678901234567", "SUCCESS", "100%", ""), done}},
		{Name: "render events progress", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(l20, "", clientBody, nil), taskView("jimeng", "QUEUED", "10%", "", nil), nil}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(l20, "", clientBody, videoArtifacts()), view, obj{"status": "QUEUED", "progress": 10}}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(l20, "", clientBody, videoArtifacts()), view}},
		{Name: "native render task", Hook: "native", Member: "renderTask",
			Args: arr{routeCtx("POST", "/jimeng/", nil, obj{"Action": arr{"CVSync2AsyncGetResult"}}, jsonBody(obj{"task_id": publicTaskID})), arr{view}}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, view}},
		{Name: "list artifacts", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "7312345678901234567", "SUCCESS", "text_to_video", done, nil, "")}},
		{Name: "build content request", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "7312345678901234567", "SUCCESS", "text_to_video", done, nil, "video")}},
	}
}
