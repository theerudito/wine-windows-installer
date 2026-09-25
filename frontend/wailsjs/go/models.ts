export namespace main {
	
	export class DesktopRequest {
	    installerPath: string;
	    targetPath: string;
	    name: string;
	    iconPath: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installerPath = source["installerPath"];
	        this.targetPath = source["targetPath"];
	        this.name = source["name"];
	        this.iconPath = source["iconPath"];
	    }
	}
	export class OperationResult {
	    success: boolean;
	    message: string;
	    output?: string;
	
	    static createFrom(source: any = {}) {
	        return new OperationResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.output = source["output"];
	    }
	}
	export class WineStatus {
	    installed: boolean;
	    version: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new WineStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.message = source["message"];
	    }
	}

}

