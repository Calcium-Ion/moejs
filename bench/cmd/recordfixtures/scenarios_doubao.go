package main

func doubaoScenarios() []scenario {
	c := creds{baseURL: "https://ark.cn-beijing.volces.com", apiKey: "ark-0123456789abcdef-0123456789abcdef"}
	const seedance = "doubao-seedance-1-0-pro-250528"
	const seedance2 = "doubao-seedance-2-0-260128"
	const seedream4 = "doubao-seedream-4-0-250828"
	const seedream5 = "doubao-seedream-5-0-lite-260128"
	img := "https://img.example.com/reference/cat.jpg"
	prompt := "A ginger cat leaping between rooftops in a rainy neon city, slow motion"

	videoRequest := obj{"model": seedance, "prompt": prompt, "metadata": obj{"resolution": "720p", "ratio": "16:9"}, "seconds": 5}
	videoBody := obj{"model": seedance, "input": prompt, "seconds": 5, "size": "1280x720"}
	imageRequest := obj{"model": seedream4, "prompt": "Poster of a jazz trio, art deco style", "size": "2K", "watermark": true, "response_format": "url"}
	video2Request := obj{"model": seedance2, "prompt": prompt, "metadata": obj{"resolution": "1080p", "content": arr{obj{"type": "video_url", "video_url": obj{"url": "https://img.example.com/ref.mp4"}}}}, "seconds": 5}
	imageResult := obj{"model": seedream4, "created": 1700000100, "data": arr{obj{"url": "https://ark-content.example.com/img/1.png", "size": "2048x2048"}, obj{"url": "https://ark-content.example.com/img/2.png", "size": "1024x1024"}}, "usage": obj{"generated_images": 2, "output_tokens": 8192, "total_tokens": 8192, "input_images": 1}}
	videoSucceeded := obj{"id": "cgt-20231114-abcdef", "model": seedance, "status": "succeeded", "content": obj{"video_url": "https://ark-content.example.com/video/x.mp4", "last_frame_url": "https://ark-content.example.com/video/x-last.png"}, "usage": obj{"completion_tokens": 108000, "total_tokens": 108000}, "created_at": 1700000000, "updated_at": 1700000120, "resolution": "720p"}

	return []scenario{
		{Name: "responses decode video", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", seedance, "", jsonBody(videoBody))}},
		{Name: "responses decode image seedream 5 lite", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", seedream5, "", jsonBody(obj{"model": seedream5, "input": responsesInput([]string{"Poster of a jazz trio, art deco style"}, img), "size": "2K", "sequential_image_generation": "auto", "sequential_image_generation_options": obj{"max_images": 3}, "tools": arr{obj{"type": "web_search"}, obj{"type": "function", "name": "x"}}}))}},
		{Name: "responses decode rejects tools on seedream 4", Hook: "protocols", Path: []string{"openai_responses", "decodeRequest"},
			Args: arr{protocolCtx("openai_responses", "create", seedream4, "", jsonBody(obj{"model": seedream4, "input": "a cat", "tools": arr{obj{"type": "web_search"}}}))}},
		{Name: "video decode json image-to-video", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", seedance2, "", jsonBody(obj{"model": seedance2, "prompt": prompt, "image": img, "seconds": 5}))}},
		{Name: "video decode rejects out-of-range seconds", Hook: "protocols", Path: []string{"openai_video", "decodeRequest"},
			Args: arr{protocolCtx("openai_video", "create", seedance, "", jsonBody(obj{"model": seedance, "prompt": prompt, "seconds": 5000}))}},
		{Name: "native create task", Hook: "native", Member: "createTask",
			Args: arr{routeCtx("POST", "/doubao/api/v3/contents/generations/tasks", nil, nil,
				jsonBody(obj{"model": seedance, "content": arr{obj{"type": "text", "text": prompt}, obj{"type": "image_url", "image_url": obj{"url": img}}}, "duration": 5, "resolution": "720p", "ratio": "16:9"}))}},
		{Name: "native create image", Hook: "native", Member: "createImage",
			Args: arr{routeCtx("POST", "/doubao/api/v3/images/generations", nil, nil, jsonBody(imageRequest))}},
		{Name: "build submit video", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: videoBody, requestBody: videoRequest, action: "text_to_video", model: seedance})}},
		{Name: "build submit image", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: imageRequest, requestBody: imageRequest, action: "text_to_image", model: seedream4})}},
		{Name: "build submit rejects 4k on seedance 1.0", Hook: "buildSubmitRequest",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: obj{"model": seedance, "prompt": prompt, "metadata": obj{"resolution": "4k"}}, action: "text_to_video", model: seedance})}},
		{Name: "parse submit video", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: videoBody, requestBody: videoRequest, action: "text_to_video", model: seedance}),
				submitResp(200, obj{"id": "cgt-20231114-abcdef"})}},
		{Name: "parse submit image immediate", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: imageRequest, requestBody: imageRequest, action: "text_to_image", model: seedream4}), submitResp(200, imageResult)}},
		{Name: "parse submit image error", Hook: "parseSubmitResponse",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: imageRequest, requestBody: imageRequest, action: "text_to_image", model: seedream4}),
				submitResp(400, obj{"error": obj{"code": "InvalidParameter", "message": "The parameter `size` specified in the request are not valid"}})}},
		{Name: "usage facts video", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: videoBody, requestBody: videoRequest, action: "text_to_video", model: seedance, usagePurpose: "facts"})}},
		{Name: "usage facts image", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: imageRequest, requestBody: imageRequest, action: "text_to_image", model: seedream4, usagePurpose: "facts"})}},
		{Name: "usage billing ratios video input", Hook: "extractUsage",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: obj{}, requestBody: video2Request, action: "image_to_video", model: seedance2, usagePurpose: "billing_ratios"})}},
		{Name: "usage on submit image count", Hook: "extractUsageOnSubmit",
			Args: arr{submitCtx(submitOpts{creds: c, clientBody: imageRequest, requestBody: imageRequest, action: "text_to_image", model: seedream4}), imageResult}},
		{Name: "build query", Hook: "buildQueryRequest",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "cgt-20231114-abcdef", action: "text_to_video", model: seedance})}},
		{Name: "parse task succeeded", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "cgt-20231114-abcdef", action: "text_to_video", model: seedance}), videoSucceeded, pollResp(200)}},
		{Name: "parse task failed", Hook: "parseTaskResult",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "cgt-20231114-abcdef", action: "text_to_video", model: seedance}),
				obj{"id": "cgt-20231114-abcdef", "status": "failed", "error": obj{"code": "OutputVideoSensitiveContentDetected", "message": "The generated video may contain sensitive information."}}, pollResp(200)}},
		{Name: "usage on complete video", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: "cgt-20231114-abcdef", action: "text_to_video", model: seedance}), taskInfo("cgt-20231114-abcdef", "SUCCESS", "100%", "https://ark-content.example.com/video/x.mp4"), videoSucceeded}},
		{Name: "usage on complete image tiers", Hook: "extractUsageOnComplete",
			Args: arr{queryCtx(queryOpts{creds: c, taskID: publicTaskID, action: "text_to_image", model: seedream4}), taskInfo(publicTaskID, "SUCCESS", "100%", ""), imageResult}},
		{Name: "render events success video", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(seedance, "", videoBody, videoArtifacts()), taskView("doubao", "SUCCESS", "100%", "", videoSucceeded), nil}},
		{Name: "render events queued", Hook: "protocols", Path: []string{"openai_responses", "renderEvents"},
			Args: arr{renderCtx(seedance, "", videoBody, nil), taskView("doubao", "QUEUED", "10%", "", nil), nil}},
		{Name: "render final images", Hook: "protocols", Path: []string{"openai_responses", "renderFinal"},
			Args: arr{renderCtx(seedream4, "", imageRequest, imageArtifacts(2)), taskView("doubao", "SUCCESS", "100%", "", imageResult)}},
		{Name: "native task status", Hook: "native", Member: "taskStatus",
			Args: arr{routeCtx("GET", "/doubao/api/v3/contents/generations/tasks/task_pub_0001", obj{"task_id": publicTaskID}, nil, nil), taskView("doubao", "SUCCESS", "100%", "", videoSucceeded)}},
		{Name: "video render", Hook: "protocols", Path: []string{"openai_video", "render"},
			Args: arr{obj{"protocol": "openai_video", "operation": "retrieve"}, taskView("doubao", "FAILURE", "100%", "sensitive", obj{"status": "failed", "error": obj{"code": "OutputVideoSensitiveContentDetected", "message": "sensitive"}})}},
		{Name: "list artifacts video", Hook: "listArtifacts",
			Args: arr{artifactCtx(c, publicTaskID, "cgt-20231114-abcdef", "SUCCESS", "text_to_video", videoSucceeded, nil, "")}},
		{Name: "build content request last frame", Hook: "buildContentRequest",
			Args: arr{artifactCtx(c, publicTaskID, "cgt-20231114-abcdef", "SUCCESS", "text_to_video", videoSucceeded, nil, "last_frame")}},
	}
}
