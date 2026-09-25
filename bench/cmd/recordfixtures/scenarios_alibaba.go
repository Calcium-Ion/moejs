package main

func alibabaScenarios() []scenario {
	c := creds{baseURL: "https://dashscope.aliyuncs.com", apiKey: "sk-ali-0123456789abcdef0123456789abcdef"}
	gw := creds{baseURL: "https://gateway.example.com", apiKey: "sk-gw-0123456789abcdef", viaNewAPI: true}
	const t2v = "wan2.6-t2v"
	const img27 = "wan2.7-image"
	const qwen = "qwen-image-plus"
	const qwen3 = "qwen-image-3.0"
	img := "https://img.example.com/reference/panda.png"
	prompt := "A giant panda practising tai chi in a misty bamboo forest at dawn, cinematic lighting"

	t2vRequest := obj{"model": t2v, "prompt": prompt, "size": "1280x720", "duration": 5}
	t2vBody := obj{"model": t2v, "input": prompt, "size": "1280x720", "duration": 5}
	qwenRequest := obj{"model": qwen, "prompt": "一只戴着墨镜的柴犬在海边冲浪", "n": 1, "size": "1024x1024"}
	img27Request := obj{"model": img27, "prompt": "Turn this photo into a watercolor painting", "images": arr{img}, "n": 2, "size": "1280*1280"}
	qwen3Request := obj{"model": qwen3, "prompt": "A poster for a jazz festival", "n": 2, "size": "1328*1328"}

	videoDone := obj{"request_id": "req-ali-1", "output": obj{"task_id": "ali-task-1", "task_status": "SUCCEEDED", "video_url": "https://dashscope-result.oss.aliyuncs.com/video/abc.mp4?Expires=1700003600&Signature=x", "submit_time": "2023-11-14 22:13:20", "end_time": "2023-11-14 22:15:00"}, "usage": obj{"duration": 5, "SR": "720P", "video_count": 1}}
	imageChoices := obj{"request_id": "req-ali-img", "output": obj{"task_status": "SUCCEEDED", "choices": arr{obj{"finish_reason": "stop", "message": obj{"role": "assistant", "content": arr{obj{"image": "https://dashscope-result.oss.aliyuncs.com/img/1.png"}, obj{"image": "https://dashscope-result.oss.aliyuncs.com/img/2.png"}}}}}}, "usage": obj{"image_count": 2}}
	successView := taskView("alibaba", "SUCCESS", "100%", "", videoDone)

	return []scenario{
		{Name: "responses decode text-to-video", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", t2v, "", jsonBody(t2vBody))}},
		{Name: "responses decode image edit wan2.7-image", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", img27, "", jsonBody(obj{"model": img27, "input": responsesInput([]string{"Turn this photo into a watercolor painting"}, img), "n": 2, "size": "1280*1280"}))}},
		{Name: "responses decode rejects empty input", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", t2v, "", jsonBody(obj{"model": t2v, "input": arr{}}))}},
		{Name: "video decode json image-to-video", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", "wan2.6-i2v", "", jsonBody(obj{"model": "wan2.6-i2v", "prompt": prompt, "image": img, "resolution": "720p", "seconds": 5}))}},
		{Name: "video decode multipart fields", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", t2v, "", multipartBody(obj{"prompt": arr{prompt}, "seconds": arr{"10"}, "size": arr{"1920x1080"}, "prompt_extend": arr{"false"}}, arr{}))}},
		{Name: "image decode qwen-image-plus", Hook: "protocols", Path: []string{"openai_image", "decodeRequest"},
			Args: arr{protocolCtx("openai_image", "create", qwen, "", jsonBody(obj{"model": qwen, "prompt": "一只戴着墨镜的柴犬在海边冲浪", "n": 1, "size": "1024x1024", "response_format": "url"}))}},
		{Name: "image decode edit requires image", Hook: "protocols", Path: []string{"openai_image", "decodeRequest"},
			Args: arr{protocolCtx("openai_image", "edit", "qwen-image-edit", "", jsonBody(obj{"model": "qwen-image-edit", "prompt": "make it night"}))}},
		{Name: "build submit text-to-video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: t2vBody, requestBody: t2vRequest, action: "text_to_video", model: t2v})}},
		{Name: "build submit qwen sync image", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, protocol: "openai_image", clientBody: qwenRequest, requestBody: qwenRequest, action: "text_to_image", model: qwen})}},
		{Name: "build submit wan2.7 async image edit", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, protocol: "openai_responses", clientBody: img27Request, requestBody: img27Request, action: "image_to_image", model: img27})}},
		{Name: "build submit via gateway", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: gw, clientBody: t2vBody, requestBody: t2vRequest, action: "text_to_video", model: t2v})}},
		{Name: "build submit rejects unsupported resolution", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": "wan2.2-t2v-plus", "prompt": prompt, "resolution": "720P"}, action: "text_to_video", model: "wan2.2-t2v-plus"})}},
		{Name: "sse delta first event", Hook: "parseSubmitEventDelta",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: qwenRequest, requestBody: qwenRequest, action: "text_to_image", model: qwen}),
				obj{"event": "result", "id": "1", "data": `{"output":{"choices":[{"message":{"role":"assistant","content":[{"text":"Here is"}]},"finish_reason":"null"}]},"usage":{"image_count":0},"request_id":"req-sse-1"}`},
				nil}},
		{Name: "sse delta final event", Hook: "parseSubmitEventDelta",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: qwenRequest, requestBody: qwenRequest, action: "text_to_image", model: qwen}),
				obj{"event": "result", "id": "2", "data": `{"output":{"choices":[{"message":{"role":"assistant","content":[{"text":" your image."},{"image":"https://dashscope-result.oss.aliyuncs.com/img/1.png"}]},"finish_reason":"stop"}]},"usage":{"image_count":1},"request_id":"req-sse-1"}`},
				obj{"choices": arr{obj{"count": 1, "lastText": true, "finishReason": ""}}, "hasUsage": true}}},
		{Name: "sse delta rejects upstream error", Hook: "parseSubmitEventDelta",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: qwenRequest, requestBody: qwenRequest, action: "text_to_image", model: qwen}),
				obj{"event": "error", "data": `{"code":"Throttling.RateQuota","message":"Requests rate limit exceeded"}`}, nil}},
		{Name: "parse submit video task", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: t2vBody, requestBody: t2vRequest, action: "text_to_video", model: t2v}),
				submitResp(200, obj{"request_id": "req-ali-1", "output": obj{"task_id": "ali-task-1", "task_status": "PENDING"}})}},
		{Name: "parse submit sync image immediate", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, protocol: "openai_image", clientBody: qwenRequest, requestBody: qwenRequest, action: "text_to_image", model: qwen}),
				submitResp(200, obj{"request_id": "req-ali-img", "output": obj{"choices": arr{obj{"finish_reason": "stop", "message": obj{"role": "assistant", "content": arr{obj{"image": "https://dashscope-result.oss.aliyuncs.com/img/1.png"}}}}}}, "usage": obj{"image_count": 1, "width": 1024, "height": 1024}})}},
		{Name: "parse submit rejects error envelope", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: t2vBody, requestBody: t2vRequest, action: "text_to_video", model: t2v}),
				submitResp(400, obj{"code": "InvalidParameter", "message": "The size is not supported", "request_id": "req-ali-2"})}},
		{Name: "usage facts video", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: t2vBody, requestBody: t2vRequest, action: "text_to_video", model: t2v, usagePurpose: "facts"})}},
		{Name: "usage facts qwen-image-3.0", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, protocol: "openai_image", clientBody: qwen3Request, requestBody: qwen3Request, action: "text_to_image", model: qwen3, usagePurpose: "facts"})}},
		{Name: "usage billing ratios wan2.5 1080P", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": "wan2.5-t2v-preview", "prompt": prompt, "resolution": "1080P", "duration": 10}, action: "text_to_video", model: "wan2.5-t2v-preview", usagePurpose: "billing_ratios"})}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-1", action: "text_to_video", model: t2v})}},
		{Name: "parse task succeeded video", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-1", action: "text_to_video", model: t2v}), videoDone, pollResp(200)}},
		{Name: "parse task failed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-1", action: "text_to_video", model: t2v}),
				obj{"request_id": "req-ali-1", "output": obj{"task_id": "ali-task-1", "task_status": "FAILED", "code": "DataInspectionFailed", "message": "Input data may contain inappropriate content."}}, pollResp(200)}},
		{Name: "parse task succeeded image choices", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-img", action: "image_to_image", model: img27}), imageChoices, pollResp(200)}},
		{Name: "usage on complete video", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-1", action: "text_to_video", model: t2v}), taskInfo("ali-task-1", "SUCCESS", "100%", ""), videoDone}},
		{Name: "usage on complete wan3.0 sums durations", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-3", action: "image_to_video", model: "wan3.0-video"}), taskInfo("ali-task-3", "SUCCESS", "100%", ""),
				obj{"output": obj{"task_status": "SUCCEEDED", "video_url": "https://x/y.mp4"}, "usage": obj{"input_video_duration": 4, "output_video_duration": 8, "SR": "1080P"}}}},
		{Name: "usage on complete image count", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "ali-task-img", action: "image_to_image", model: img27}), taskInfo("ali-task-img", "SUCCESS", "100%", ""), imageChoices}},
		{Name: "render events success video", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(t2v, "", t2vBody, videoArtifacts()), successView, obj{"status": "IN_PROGRESS", "progress": nil}}},
		{Name: "render events progress unchanged", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(t2v, "", t2vBody, nil), taskView("alibaba", "IN_PROGRESS", "", "", nil), obj{"status": "IN_PROGRESS", "progress": nil}}},
		{Name: "render final images", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(img27, "", img27Request, imageArtifacts(2)), taskView("alibaba", "SUCCESS", "100%", "", imageChoices)}},
		{Name: "native create video task", Hook: "native", Member: "createVideoTask",
			Args: arr{routeCtx("POST", "/ali/api/v1/services/aigc/video-generation/video-synthesis", nil, nil,
				jsonBody(obj{"model": "wan2.6-i2v", "input": obj{"prompt": prompt, "img_url": img}, "parameters": obj{"resolution": "720p", "duration": 5, "prompt_extend": true}}))}},
		{Name: "native create image task sync", Hook: "native", Member: "createImageTask",
			Args: arr{routeCtx("POST", "/ali/api/v1/services/aigc/multimodal-generation/generation", nil, nil,
				jsonBody(obj{"model": qwen, "input": obj{"messages": arr{obj{"role": "user", "content": arr{obj{"text": "a cat"}}}}}, "parameters": obj{"size": "1024*1024", "n": 1}}))}},
		{Name: "native task status", Hook: "native", Member: "taskStatus",
			Args: arr{routeCtx("GET", "/ali/api/v1/tasks/task_pub_0001", obj{"task_id": publicTaskID}, nil, nil), successView}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, successView}},
		{Name: "image render", Hook: "protocols", Path: []string{"openai_image", "render"},
			Args: arr{obj{"protocol": "openai_image", "operation": "retrieve"}, taskView("alibaba", "SUCCESS", "100%", "", imageChoices)}},
		{Name: "list artifacts images", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "ali-task-img", "SUCCESS", "image_to_image", imageChoices, nil, "")}},
		{Name: "build content request video", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "ali-task-1", "SUCCESS", "text_to_video", videoDone, nil, "video")}},
	}
}
