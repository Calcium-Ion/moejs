package main

func viduScenarios() []scenario {
	c := creds{baseURL: "https://api.vidu.com", apiKey: "vda_0123456789abcdef0123456789abcdef"}
	const q2 = "viduq2"
	const q1 = "viduq1"
	const v20 = "vidu2.0"
	img := "https://img.example.com/reference/astronaut.png"
	img2 := "https://img.example.com/reference/astronaut-end.png"
	img3 := "https://img.example.com/reference/helmet.png"
	prompt := "An astronaut planting a flag on a candy-colored planet"
	clientBody := obj{"model": q2, "input": prompt, "seconds": 5, "size": "1920x1080"}
	request := obj{"model": q2, "prompt": prompt, "duration": 5, "size": "1920x1080"}
	i2vRequest := obj{"model": q2, "prompt": prompt, "images": arr{img}, "duration": 5, "metadata": obj{"resolution": "720p", "bgm": false, "seed": 42}}
	refRequest := obj{"model": q2, "prompt": prompt, "images": arr{img, img2, img3}, "duration": 5}
	success := obj{"id": "vd-1", "state": "success", "err_code": "", "credits": 40, "payload": "", "creations": arr{obj{"id": "cr-1", "url": "https://prod-ss-vidu.example.com/cr-1.mp4?Expires=1700003600", "cover_url": "https://prod-ss-vidu.example.com/cr-1.jpg"}}}
	view := taskView("vidu", "SUCCESS", "100%", "", success)

	return []scenario{
		{Name: "responses decode q2 text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", q2, "", jsonBody(clientBody))}},
		{Name: "responses decode 2.0 first tail", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", v20, "", jsonBody(obj{"model": v20, "input": responsesInput([]string{prompt}, img, img2), "seconds": 4}))}},
		{Name: "responses decode rejects 2.0 text", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", v20, "", jsonBody(obj{"model": v20, "input": prompt}))}},
		{Name: "video decode q1 image", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", q1, "", jsonBody(obj{"model": q1, "prompt": prompt, "image": img, "seconds": 5}))}},
		{Name: "video decode rejects q1 duration", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", q1, "", jsonBody(obj{"model": q1, "prompt": prompt, "seconds": 8}))}},
		{Name: "video decode multipart file", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", q2, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"5"}, "metadata": arr{`{"resolution":"720p"}`}}, arr{obj{"field": "input_reference", "ref": "request_file:input_reference:0", "filename": "a.png", "mimeType": "image/png", "size": 1234}}))}},
		{Name: "build submit image-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: q2})}},
		{Name: "build submit reference-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: refRequest, action: "reference_to_video", model: q2})}},
		{Name: "build submit text q1", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: obj{"model": q1, "prompt": prompt, "duration": 5}, action: "text_to_video", model: q1})}},
		{Name: "parse submit", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: q2}), submitResp(200, obj{"task_id": "vd-1", "state": "created", "model": q2, "prompt": prompt, "duration": 5, "resolution": "720p", "created_at": "2023-11-14T22:13:20Z"})}},
		{Name: "parse submit rejects failed state", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: clientBody, requestBody: request, action: "text_to_video", model: q2}), submitResp(200, obj{"task_id": "vd-2", "state": "failed", "err_code": "ContentModeration"})}},
		{Name: "usage facts", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: q2, usagePurpose: "facts"})}},
		{Name: "usage billing ratios is null", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: i2vRequest, action: "image_to_video", model: q2, usagePurpose: "billing_ratios"})}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "vd-1", action: "image_to_video", model: q2})}},
		{Name: "parse task success", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "vd-1", action: "image_to_video", model: q2}), success, pollResp(200)}},
		{Name: "parse task failed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "vd-1", action: "image_to_video", model: q2}), obj{"id": "vd-1", "state": "failed", "err_code": "ContentModeration", "creations": arr{}}, pollResp(200)}},
		{Name: "parse task unknown", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "vd-1", action: "image_to_video", model: q2}), obj{"id": "vd-1", "state": "scheduled"}, pollResp(200)}},
		{Name: "usage on complete credits", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "vd-1", action: "image_to_video", model: q2}), taskInfo("vd-1", "SUCCESS", "100%", ""), success}},
		{Name: "render events success", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(q2, "", clientBody, videoArtifacts()), view, nil}},
		{Name: "render events submitted", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(q2, "", clientBody, nil), taskView("vidu", "SUBMITTED", "", "", nil), nil}},
		{Name: "render final", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(q2, "", clientBody, videoArtifacts()), view}},
		{Name: "video render failed", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, taskView("vidu", "FAILURE", "100%", "ContentModeration", obj{"state": "failed", "err_code": "ContentModeration"})}},
		{Name: "list artifacts", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "vd-1", "SUCCESS", "image_to_video", success, nil, "")}},
		{Name: "build content request", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "vd-1", "SUCCESS", "image_to_video", success, nil, "video")}},
	}
}
