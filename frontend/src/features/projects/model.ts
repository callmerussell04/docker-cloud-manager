export interface ProjectData {
  id: string;
  name: string;
  status: string;
  error_message: string;
  last_error: string;
  created_at: number;
}

export interface AdminProjectData extends ProjectData {
  owner_id?: string;
  owner_username?: string;
}

export interface CreateProjectGitPayload {
  project_name: string;
  repo_url: string;
  ref?: string;
  compose_file?: string;
}
