- Resolve the Curator engine catalog symlinks once in the SAM loader before
  parsing, and export that same resolved path so later directory or leaf link
  retargets cannot redirect ensure/status consumers to another catalog.
