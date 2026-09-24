--
-- PostgreSQL database dump
--

\restrict LpYrNXRak7OgjZ6oaEeecj4mbgPenH93cd4D5lEituEzlKrsPx7UmX24peOHCpj

-- Dumped from database version 17.9 (Debian 17.9-1.pgdg13+1)
-- Dumped by pg_dump version 17.9 (Debian 17.9-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: agent_memories; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.agent_memories (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL,
    tenant_id character varying(36) NOT NULL,
    kb_id character varying(36) NOT NULL,
    user_id character varying(36) DEFAULT ''::character varying NOT NULL,
    session_id character varying(36) DEFAULT ''::character varying NOT NULL,
    content text NOT NULL,
    memory_type character varying(32) DEFAULT 'episodic'::character varying NOT NULL,
    importance integer DEFAULT 0 NOT NULL,
    tier smallint DEFAULT 1 NOT NULL,
    embedding public.vector(2048),
    fingerprint character varying(64),
    tags text[] DEFAULT '{}'::text[],
    metadata jsonb DEFAULT '{}'::jsonb,
    access_count bigint DEFAULT 0 NOT NULL,
    last_accessed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    expires_at timestamp with time zone,
    verdict character varying(16) DEFAULT 'none'::character varying,
    hub_score real DEFAULT 0
);


ALTER TABLE public.agent_memories OWNER TO postgres;

--
-- Name: TABLE agent_memories; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON TABLE public.agent_memories IS 'Core memory storage for Memory v2 module';


--
-- Name: COLUMN agent_memories.content; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON COLUMN public.agent_memories.content IS 'Memory content text (10-10000 chars)';


--
-- Name: COLUMN agent_memories.memory_type; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON COLUMN public.agent_memories.memory_type IS 'episodic, semantic, procedural, decision, preference, fact';


--
-- Name: COLUMN agent_memories.importance; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON COLUMN public.agent_memories.importance IS 'Importance score (-5 to +6)';


--
-- Name: COLUMN agent_memories.tier; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON COLUMN public.agent_memories.tier IS 'Storage tier: 0=critical, 1=core, 2=standard, 3=edge';


--
-- Name: COLUMN agent_memories.fingerprint; Type: COMMENT; Schema: public; Owner: postgres
--

COMMENT ON COLUMN public.agent_memories.fingerprint IS 'SHA256 of first 200 normalized chars for structural dedup';


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


ALTER TABLE public.schema_migrations OWNER TO postgres;

--
-- Name: wiki_pages; Type: TABLE; Schema: public; Owner: postgres
--

CREATE TABLE public.wiki_pages (
    id character varying(36) NOT NULL,
    tenant_id bigint NOT NULL,
    knowledge_base_id character varying(36) NOT NULL,
    slug character varying(255) NOT NULL,
    title character varying(512) DEFAULT ''::character varying NOT NULL,
    page_type character varying(32) DEFAULT 'summary'::character varying NOT NULL,
    status character varying(32) DEFAULT 'published'::character varying NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    source_refs jsonb DEFAULT '[]'::jsonb,
    chunk_refs jsonb DEFAULT '[]'::jsonb,
    in_links jsonb DEFAULT '[]'::jsonb,
    out_links jsonb DEFAULT '[]'::jsonb,
    page_metadata jsonb DEFAULT '{}'::jsonb,
    aliases jsonb DEFAULT '[]'::jsonb,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    parent_slug character varying(255) DEFAULT ''::character varying NOT NULL,
    category_path jsonb DEFAULT '[]'::jsonb,
    wiki_path character varying(1024) DEFAULT ''::character varying NOT NULL,
    depth integer DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    folder_id character varying(36) DEFAULT ''::character varying NOT NULL
);


ALTER TABLE public.wiki_pages OWNER TO postgres;

--
-- Name: agent_memories agent_memories_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.agent_memories
    ADD CONSTRAINT agent_memories_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: wiki_pages wiki_pages_pkey; Type: CONSTRAINT; Schema: public; Owner: postgres
--

ALTER TABLE ONLY public.wiki_pages
    ADD CONSTRAINT wiki_pages_pkey PRIMARY KEY (id);


--
-- Name: idx_agent_memories_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_deleted_at ON public.agent_memories USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);


--
-- Name: idx_agent_memories_expires_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_expires_at ON public.agent_memories USING btree (tenant_id, kb_id, expires_at) WHERE (deleted_at IS NULL);


--
-- Name: idx_agent_memories_fingerprint; Type: INDEX; Schema: public; Owner: postgres
--

CREATE UNIQUE INDEX idx_agent_memories_fingerprint ON public.agent_memories USING btree (fingerprint) WHERE ((fingerprint IS NOT NULL) AND (deleted_at IS NULL));


--
-- Name: idx_agent_memories_fts; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_fts ON public.agent_memories USING bm25 (id, tenant_id, kb_id, content, memory_type, importance, tier, tags) WITH (key_field=id);


--
-- Name: idx_agent_memories_session; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_session ON public.agent_memories USING btree (tenant_id, session_id);


--
-- Name: idx_agent_memories_session_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_session_id ON public.agent_memories USING btree (tenant_id, session_id);


--
-- Name: idx_agent_memories_tags; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_tags ON public.agent_memories USING gin (tags);


--
-- Name: idx_agent_memories_tenant_kb; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_tenant_kb ON public.agent_memories USING btree (tenant_id, kb_id);


--
-- Name: idx_agent_memories_tenant_kb_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_tenant_kb_type ON public.agent_memories USING btree (tenant_id, kb_id, memory_type);


--
-- Name: idx_agent_memories_user_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_user_id ON public.agent_memories USING btree (tenant_id, user_id);


--
-- Name: idx_agent_memories_verdict; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_agent_memories_verdict ON public.agent_memories USING btree (tenant_id, verdict);


--
-- Name: idx_wiki_pages_deleted_at; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_deleted_at ON public.wiki_pages USING btree (deleted_at);


--
-- Name: idx_wiki_pages_folder_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_folder_id ON public.wiki_pages USING btree (folder_id);


--
-- Name: idx_wiki_pages_fulltext; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_fulltext ON public.wiki_pages USING gin (to_tsvector('simple'::regconfig, (((COALESCE(title, ''::character varying))::text || ' '::text) || COALESCE(content, ''::text))));


--
-- Name: idx_wiki_pages_kb_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_kb_id ON public.wiki_pages USING btree (knowledge_base_id);


--
-- Name: idx_wiki_pages_kb_slug; Type: INDEX; Schema: public; Owner: postgres
--

CREATE UNIQUE INDEX idx_wiki_pages_kb_slug ON public.wiki_pages USING btree (knowledge_base_id, slug) WHERE (deleted_at IS NULL);


--
-- Name: idx_wiki_pages_page_type; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_page_type ON public.wiki_pages USING btree (knowledge_base_id, page_type);


--
-- Name: idx_wiki_pages_parent_slug; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_parent_slug ON public.wiki_pages USING btree (knowledge_base_id, parent_slug);


--
-- Name: idx_wiki_pages_source_refs; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_source_refs ON public.wiki_pages USING gin (source_refs jsonb_path_ops);


--
-- Name: idx_wiki_pages_source_refs_text; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_source_refs_text ON public.wiki_pages USING gin (to_tsvector('simple'::regconfig, (source_refs)::text));


--
-- Name: idx_wiki_pages_tenant_id; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_tenant_id ON public.wiki_pages USING btree (tenant_id);


--
-- Name: idx_wiki_pages_title_trgm; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_title_trgm ON public.wiki_pages USING gin (lower((title)::text) public.gin_trgm_ops);


--
-- Name: idx_wiki_pages_tree; Type: INDEX; Schema: public; Owner: postgres
--

CREATE INDEX idx_wiki_pages_tree ON public.wiki_pages USING btree (knowledge_base_id, page_type, wiki_path, sort_order, title);


--
-- Name: agent_memories trg_agent_memories_updated_at; Type: TRIGGER; Schema: public; Owner: postgres
--

CREATE TRIGGER trg_agent_memories_updated_at BEFORE UPDATE ON public.agent_memories FOR EACH ROW EXECUTE FUNCTION public.update_agent_memories_updated_at();


--
-- PostgreSQL database dump complete
--

\unrestrict LpYrNXRak7OgjZ6oaEeecj4mbgPenH93cd4D5lEituEzlKrsPx7UmX24peOHCpj

